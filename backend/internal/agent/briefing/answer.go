package briefing

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing/declined"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// FieldMaxRunes is the longest value of a required Brief item.
const FieldMaxRunes = 200

// ValueProblem says why value cannot be the Brief item f ("" when it can). It is the rule of the
// Brief form (PUT /projects/:id/brief) and of the answers given to the questions in the chat.
func ValueProblem(f domain.BriefField, value string) string {
	if value == domain.DelegateOption && !f.CanDelegate() {
		return "「" + domain.DelegateOption + "」は選べません。AI が題材を決めると、指定と無関係な内容になるためです"
	}
	if utf8.RuneCountInString(value) > FieldMaxRunes {
		return fmt.Sprintf("%d 文字以内で入力してください", FieldMaxRunes)
	}
	if f == domain.BriefAspectRatio && value != "" && !domain.IsBriefAspectRatio(value) {
		return strings.Join(domain.BriefAspectRatios(), " / ") + " から選んでください"
	}
	return ""
}

// Question is the Question card about the Brief item f, with its quick-answer buttons.
func Question(f domain.BriefField, text string) domain.ChatMessage {
	return domain.ChatMessage{
		Role: domain.RoleAssistant, Kind: domain.KindQuestion,
		Content: domain.EncodeCardBody(domain.CardBody{Text: text, Options: f.Options()}),
		RefType: domain.RefBriefField, RefID: string(f),
	}
}

// ReferenceProblem says why the URL u cannot be a Brief Reference ("" when it can). It is the rule of
// the Brief form (PUT /projects/:id/brief) and of the URLs RuleExtractor takes from the chat, so a URL
// pasted in the chat never blocks saving the form.
func ReferenceProblem(u string) string {
	if utf8.RuneCountInString(u) > FieldMaxRunes || strings.ContainsAny(u, " \t") {
		return fmt.Sprintf("URL が %d 文字を超えているか、空白を含んでいます", FieldMaxRunes)
	}
	return ""
}

// vagueSubjectProblem is why a Subject answer is refused when it names nothing concrete (#1114).
const vagueSubjectProblem = "題材（主役）として読み取れませんでした。「ただの投稿動画」や「商品ではなく」のような否定・一般的な言い方では、Subject は決まりません。主役・題材を具体的に教えてください"

// ApplyAnswer stores an answer the user gave to the question about f; the Brief form's rule refuses it with the question again and the reason.
// An answer to the Subject question is the Subject only when it names something concrete: "A ではなく B" is B, while a denial or a general
// phrase ("商品ではなく、ただの投稿動画") is refused and never replaces the Subject (#1114). A Subject answer also closes the open contradiction.
func ApplyAnswer(brief *domain.Brief, f domain.BriefField, text string) *domain.ChatMessage {
	text = strings.TrimSpace(text)
	if text == domain.DelegateOption && f.CanDelegate() {
		brief.Delegate(f)
		return nil
	}
	problem := ValueProblem(f, text)
	if f == domain.BriefSubject && problem == "" && text != "" {
		// The safety refusals the Subject is read without stay as a constraint, also when the answer is asked again.
		if refusals := declined.Refusals(text); len(refusals) > 0 {
			Absorb(brief, domain.Brief{AdditionalInstruction: ConstraintPrefix + strings.Join(refusals, "・")}, "")
		}
		if text = subjectAnswer(text); text == "" {
			problem = vagueSubjectProblem
		}
	}
	if problem != "" {
		again := Question(f, f.Label()+": "+problem+"\n"+f.Question())
		if f == domain.BriefSubject && brief.ProposedSubject != "" {
			again = SubjectConflict(*brief, f.Label()+": "+problem)
		}
		return &again
	}
	if text != "" {
		brief.Set(f, text)
		if f == domain.BriefSubject {
			brief.ProposedSubject = ""
		}
	}
	return nil
}

// Changed reports whether b holds something a does not: a required item, a delegation, an attachment, the additional instruction, where an item came from or the open contradiction.
func Changed(a, b domain.Brief) bool {
	return !slices.Equal(a.Assets, b.Assets) || !slices.Equal(a.References, b.References) ||
		!slices.Equal(a.Delegated, b.Delegated) || a.AdditionalInstruction != b.AdditionalInstruction ||
		!maps.Equal(a.Sources, b.Sources) || a.ProposedSubject != b.ProposedSubject ||
		slices.ContainsFunc(domain.RequiredBriefFields(), func(f domain.BriefField) bool { return a.Get(f) != b.Get(f) })
}
