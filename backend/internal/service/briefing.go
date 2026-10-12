package service

import (
	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

const briefCompleteText = "Creative Brief が揃いました。補助ビューの Brief でいつでも確認・編集できます。"

// briefFollowUp is the assistant's answer after the Brief changed: the next question
// about the first missing item (with the "AIに任せる" button added by the renderer),
// or the completion notice.
func briefFollowUp(b domain.Brief) domain.ChatMessage {
	missing := b.Missing()
	if len(missing) == 0 {
		return assistantText(briefCompleteText)
	}
	return briefing.Question(missing[0], missing[0].Question())
}

// templateFor picks the Production Template from the extracted Content Type (UC-7).
func templateFor(contentType string) domain.ProductionTemplate {
	switch contentType {
	case "ドラマ":
		return domain.TemplateDrama
	case "アニメ":
		return domain.TemplateAnime
	case "SNS/ショート動画":
		return domain.TemplateSNSShort
	}
	return domain.TemplateProductCM
}
