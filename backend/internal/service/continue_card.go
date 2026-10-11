package service

import (
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/continuation"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// candidateLabels are the letters of the NEXT candidates. D (free direction) and E (leave it to
// the AI) follow them.
const candidateLabels = "ABCDE"

func cardBody(shot domain.Shot, p continuation.Proposal) domain.CardBody {
	var text strings.Builder
	fmt.Fprintf(&text, "Shot %d の続きを作れます。最終フレーム時点の状態:\n", shot.OrderIndex+1)
	for _, r := range p.Analysis.Rows() {
		fmt.Fprintf(&text, "・%s: %s\n", r.Label, r.Value)
	}
	next := len(p.Candidates)
	fmt.Fprintf(&text, "\n次の展開を選んでください。%c は入力欄から自由に指示、%c は「%s」です。", candidateLabels[next], candidateLabels[next+1], domain.DelegateOption)
	options := make([]string, next)
	for i, c := range p.Candidates {
		options[i] = candidateOption(i, c)
	}
	return domain.CardBody{Text: text.String(), Options: options}
}

func candidateOption(i int, text string) string { return string(candidateLabels[i]) + ". " + text }

// directionOf is the direction a reply to a Continue card stands for: the candidate text when the
// reply is one of the card's options, "AIに任せる" as delegation, otherwise the user's own words.
func directionOf(card domain.CardBody, reply string) ConfirmInput {
	reply = strings.TrimSpace(reply)
	if reply == domain.DelegateOption {
		return ConfirmInput{Delegate: true}
	}
	for i := range card.Options {
		if reply == card.Options[i] {
			return ConfirmInput{Instruction: strings.TrimPrefix(reply, string(candidateLabels[i])+". ")}
		}
	}
	return ConfirmInput{Instruction: reply}
}
