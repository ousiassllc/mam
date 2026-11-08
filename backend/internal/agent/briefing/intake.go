package briefing

import (
	"maps"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing/constraint"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Prefixes of the lines the extractor writes into the Additional Instruction (spec/spec.md §4.2).
const (
	ThemePrefix      = domain.InstructionThemePrefix
	ConstraintPrefix = domain.InstructionConstraintPrefix
)

// ConfirmOption is the quick-answer button of the batch summary.
const ConfirmOption = "この内容で進める"

// confirmWords are the replies that confirm the batch summary in words: the words an Approval card accepts and the button.
var confirmWords = []string{
	"承認", "承認します", "承認です", "ok", "okay", "はい", "いいね", "いいです", "いいよ", "問題ない", "問題なし", "lgtm", "お願いします",
	"進めて", "この内容でいい", "この内容でよい", "この内容でよいです", "この内容で進める", "この内容で進めて",
}

// IsConfirmation reports whether text, apart from surrounding blanks and 。.!！, is one of the confirm words.
func IsConfirmation(text string) bool {
	return slices.Contains(confirmWords, strings.ToLower(strings.Trim(strings.TrimSpace(text), "。.!！ ")))
}

// SetFrom stores value as the Brief item f with the given source (domain.BriefSource*); BriefSourceUser is a plain Set.
func SetFrom(b *domain.Brief, f domain.BriefField, value, source string) {
	b.Set(f, value)
	if value != "" {
		markSource(b, f, source)
	}
}

// markSource records where the item f came from; the user is the default and is not stored.
func markSource(b *domain.Brief, f domain.BriefField, source string) {
	if source == domain.BriefSourceUser {
		return
	}
	if b.Sources == nil {
		b.Sources = map[string]string{}
	}
	b.Sources[string(f)] = source
}

// Unconfirmed lists the items that hold a value the user did not state or confirm (an extracted or a recommended one).
func Unconfirmed(b domain.Brief) []domain.BriefField {
	return slices.DeleteFunc(append(domain.RequiredBriefFields(), domain.BriefAdditionalInstruction), func(f domain.BriefField) bool {
		value := b.AdditionalInstruction
		if f != domain.BriefAdditionalInstruction {
			value = b.Get(f)
		}
		return value == "" || b.SourceOf(f) == domain.BriefSourceUser
	})
}

// Adopt makes every value the user's: what the user saw and accepted (the Brief form saved, the summary confirmed).
func Adopt(b *domain.Brief) { b.Sources = nil }

// LeaveOpenItemsToAI hands every item still open but the Subject (which cannot be left to the AI) to the AI.
func LeaveOpenItemsToAI(b *domain.Brief) {
	for _, f := range b.Missing() {
		if f.CanDelegate() {
			b.Delegate(f)
		}
	}
}

// Confirm is the user's answer "この内容でよい" to the batch summary: the values become the user's and the open items are left to the AI.
func Confirm(b *domain.Brief) {
	Adopt(b)
	LeaveOpenItemsToAI(b)
}

// Confirmed reports whether the Brief needs no more question: nothing open, nothing tentative, no contradiction.
func Confirmed(b domain.Brief) bool {
	return len(b.Missing()) == 0 && len(Unconfirmed(b)) == 0 && b.ProposedSubject == ""
}

// ReadyToPlan reports whether PLANNING may start (spec/spec.md §4.2). The Subject must be known and not contradicted. At FULL AUTO that is
// all: the recommended values are adopted and the open items are left to the AI. At the other levels the Brief must also be confirmed
// (the batch summary answered, or every item decided by the user).
func ReadyToPlan(b domain.Brief, level domain.ConfirmationLevel) bool {
	if slices.Contains(b.Missing(), domain.BriefSubject) || b.ProposedSubject != "" {
		return false
	}
	return level == domain.ConfirmFullAuto || Confirmed(b)
}

// Absorb folds what a free message says into the Brief (extracted is what the extractor read from text):
//   - an item nobody decided, left to the AI, or only proposed takes the stated value as inferred from the request, so an explicit value always replaces a recommendation;
//   - a value read from an earlier message is replaced by the newer statement;
//   - a value the user decided is replaced only when the clause that states the new value says it changes it (an explicit change, e.g. "…に変更して"; a change word elsewhere in the text, "30秒にして。主人公はロボット", does not count for the Subject);
//   - a different Subject without such a word never replaces the Subject: it is kept as ProposedSubject and the user is asked about that point alone.
//
// A text that states nothing concrete (the extractor read nothing) changes nothing.
func Absorb(b *domain.Brief, extracted domain.Brief, text string) {
	for _, f := range domain.RequiredBriefFields() {
		v, cur, source := extracted.Get(f), b.Get(f), b.SourceOf(f)
		explicit := v != "" && v != cur && explicitChange(text, f, v)
		switch {
		case v == "" || v == cur:
		case cur == "" || source == domain.BriefSourceRecommended:
			SetFrom(b, f, v, domain.BriefSourceInferred)
		case f == domain.BriefSubject && !explicit:
			b.ProposedSubject = v
		case explicit:
			b.Set(f, v)
		case source == domain.BriefSourceInferred:
			SetFrom(b, f, v, domain.BriefSourceInferred)
		}
		if f == domain.BriefSubject && v != "" && b.Get(f) == v {
			b.ProposedSubject = ""
		}
	}
	source := b.SourceOf(domain.BriefAdditionalInstruction)
	if b.AdditionalInstruction == "" {
		source = domain.BriefSourceInferred
	}
	inferred := source == domain.BriefSourceInferred
	current := b.AdditionalInstruction
	if inferred {
		current = constraint.Retract(current, constraint.Retracted(text))
	}
	if merged := mergeInstruction(current, extracted.AdditionalInstruction, inferred); merged != b.AdditionalInstruction {
		b.AdditionalInstruction = merged
		markSource(b, domain.BriefAdditionalInstruction, source)
	}
	for _, a := range extracted.References {
		if !slices.Contains(b.References, a) {
			b.References = append(b.References, a)
		}
	}
}

// mergeInstruction adds the lines of extra that current does not hold yet. While the instruction is only what the extractor read from the
// request (inferred), a later theme replaces the earlier one and later constraints join the earlier ones; a line the user wrote or confirmed is
// kept and the new lines are added after it.
func mergeInstruction(current, extra string, inferred bool) string {
	lines := strings.Split(current, "\n")
	for line := range strings.SplitSeq(extra, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(current, line) {
			continue
		}
		i := -1
		if inferred {
			i = slices.IndexFunc(lines, func(l string) bool {
				return (strings.HasPrefix(line, ThemePrefix) && strings.HasPrefix(l, ThemePrefix)) || (strings.HasPrefix(line, ConstraintPrefix) && strings.HasPrefix(l, ConstraintPrefix))
			})
		}
		switch {
		case i < 0:
			lines = append(lines, line)
		case strings.HasPrefix(line, ConstraintPrefix):
			lines[i] = joinConstraints(lines[i], line)
		default:
			lines[i] = line
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// joinConstraints is the constraint line holding the items of both: the labels in the order of constraint.Labels, then the stated refusals ("暴力は出さない") in the order they came.
func joinConstraints(a, b string) string {
	items := strings.Split(strings.TrimPrefix(a, ConstraintPrefix)+"・"+strings.TrimPrefix(b, ConstraintPrefix), "・")
	joined := slices.DeleteFunc(slices.Clone(constraint.Labels), func(l string) bool { return !slices.Contains(items, l) })
	for _, item := range items {
		if item != "" && !slices.Contains(joined, item) && !slices.Contains(constraint.Labels, item) {
			joined = append(joined, item)
		}
	}
	return ConstraintPrefix + strings.Join(joined, "・")
}

// NeedsConfirmation reports whether the batch summary is waiting for the user: the Brief is drafted, the Subject is settled, and
// something tentative or open is left.
func NeedsConfirmation(b domain.Brief) bool {
	return !slices.Contains(b.Missing(), domain.BriefSubject) && b.ProposedSubject == "" && !Confirmed(b)
}

// Clone copies b so that the copy can be changed and compared with it (the sources and the lists are not shared).
func Clone(b domain.Brief) domain.Brief {
	b.Sources = maps.Clone(b.Sources)
	b.Delegated, b.Assets, b.References = slices.Clone(b.Delegated), slices.Clone(b.Assets), slices.Clone(b.References)
	return b
}

// ApplyEdit puts an edited Brief form into b (the Brief form and update_brief). An item whose value differs becomes the user's, and so does
// an item in given even when its value equals the recommended or inferred one: the user stated it. Saving a form that leaves a recommended
// value alone keeps it marked as recommended, unless confirm says the user accepted every value that is shown. The contradiction on the
// Subject is settled by confirm and by a given Subject: the form shows the Subject that stays.
func ApplyEdit(b *domain.Brief, values map[domain.BriefField]string, instruction string, given []domain.BriefField, confirm bool) {
	for _, f := range domain.RequiredBriefFields() {
		// A field edited to empty is "not decided" again: the AI asks about it unless it was delegated.
		if v := strings.TrimSpace(values[f]); v != b.Get(f) || (v != "" && slices.Contains(given, f)) {
			b.Set(f, v)
		}
	}
	if instruction = strings.TrimSpace(instruction); instruction != b.AdditionalInstruction || (instruction != "" && slices.Contains(given, domain.BriefAdditionalInstruction)) {
		b.AdditionalInstruction = instruction
		delete(b.Sources, string(domain.BriefAdditionalInstruction))
	}
	if confirm || slices.Contains(given, domain.BriefSubject) {
		b.ProposedSubject = ""
	}
	if confirm {
		Adopt(b)
	}
}
