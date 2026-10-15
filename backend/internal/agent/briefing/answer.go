package briefing

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// FieldMaxRunes is the longest value of a required Brief item.
const FieldMaxRunes = 200

// ValueProblem says why value cannot be the Brief item f ("" when it can). It is the rule of the
// Brief form (PUT /projects/:id/brief) and of the answers given to the questions in the chat.
func ValueProblem(f domain.BriefField, value string) string {
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
