package briefing

import (
	"maps"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// CompleteText is the notice that the Brief needs no more question.
const CompleteText = "Creative Brief が揃いました。補助ビューの Brief でいつでも確認・編集できます。"

// Marks of the values that are not the user's yet, in the summary.
const (
	markInferred    = "（依頼文から読み取り）"
	markRecommended = "（推奨・仮の値）"
)

// mark is the note that tells a value the user did not decide ("" for the user's own value).
func mark(source string) string {
	switch source {
	case domain.BriefSourceInferred:
		return markInferred
	case domain.BriefSourceRecommended:
		return markRecommended
	}
	return ""
}

// FollowUp is the assistant's message after the Brief changed (#1114). While the Project drafts it asks only what blocks the start —
// the contradicted or unknown Subject — and otherwise shows the whole Brief once for confirmation ("この内容でよいですか？"), or, at
// FULL AUTO, says what is adopted. Once PLANNING has started (drafting is false) only the Subject can still be asked.
func FollowUp(b domain.Brief, level domain.ConfirmationLevel, drafting bool) domain.ChatMessage {
	switch {
	case b.ProposedSubject != "":
		return SubjectConflict(b, "")
	case slices.Contains(b.Missing(), domain.BriefSubject):
		text := domain.BriefSubject.Question()
		if known := knownLines(b, false); known != "" && drafting {
			text = "依頼から読み取れた内容です。\n" + known + "\n\n題材（主役）だけは決められませんでした。\n" + text
		}
		return Question(domain.BriefSubject, text)
	case !drafting || Confirmed(b):
		return text(CompleteText)
	case level == domain.ConfirmFullAuto:
		return text("Brief を次の内容にして、確認なしで制作を始めます（確認レベルが FULL AUTO のため）。違う内容にしたいときは補助ビューの Brief で直してください。\n" + knownLines(b, true))
	}
	card := domain.ChatMessage{
		Role: domain.RoleAssistant, Kind: domain.KindQuestion, RefType: domain.RefBriefConfirm, RefID: domain.RefBriefConfirm,
		Content: domain.EncodeCardBody(domain.CardBody{
			Text:    "Brief をまとめました。この内容でよいですか？\n" + knownLines(b, true) + "\n\n" + summaryHint(b),
			Options: []string{ConfirmOption},
		}),
	}
	return card
}

// summaryHint is the last paragraph of the summary: how to change an item, and what the marks mean when there are any.
func summaryHint(b domain.Brief) string {
	hint := "違う項目は、返信で値を伝えるか、補助ビューの Brief で編集してください。"
	if slices.Contains(slices.Collect(maps.Values(b.Sources)), domain.BriefSourceRecommended) {
		hint = markRecommended + "は目的から決めた仮の値です。" + hint
	}
	return hint
}

// SubjectConflict is the question about the Subject alone when the user's words named another one than the Subject decided: the
// buttons are the two values; the Brief keeps its Subject until one is picked.
func SubjectConflict(b domain.Brief, reason string) domain.ChatMessage {
	q := "Subject は「" + b.Subject + "」ですが、「" + b.ProposedSubject + "」とも読み取れました。どちらを主役にしますか？\n（変更するときは、「…に変更して」のように変更と分かる言い方でも伝えられます）"
	if reason != "" {
		q = reason + "\n" + q
	}
	m := Question(domain.BriefSubject, q)
	m.Content = domain.EncodeCardBody(domain.CardBody{Text: q, Options: []string{b.Subject, b.ProposedSubject}})
	return m
}

// knownLines lists the Brief items with their value and where it came from; with withOpen an item nobody decided says that the AI will choose it.
func knownLines(b domain.Brief, withOpen bool) string {
	var sb strings.Builder
	for _, f := range domain.RequiredBriefFields() {
		switch v := b.Get(f); {
		case v != "":
			sb.WriteString("- " + f.Label() + ": " + v + mark(b.SourceOf(f)) + "\n")
		case b.IsDelegated(f):
			sb.WriteString("- " + f.Label() + ": AIに任せる\n")
		case withOpen && f != domain.BriefSubject:
			sb.WriteString("- " + f.Label() + ": 未設定（このまま進めると AI に任せます。決めたいときは返信で教えてください）\n")
		}
	}
	if b.AdditionalInstruction != "" {
		sb.WriteString("- Additional Instruction: " + strings.ReplaceAll(b.AdditionalInstruction, "\n", " / ") + mark(b.SourceOf(domain.BriefAdditionalInstruction)) + "\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func text(content string) domain.ChatMessage {
	return domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindText, Content: content}
}
