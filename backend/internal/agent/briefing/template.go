package briefing

import "github.com/ousiassllc/mam/backend/internal/domain"

// TemplateFor picks the Production Template from the extracted Content Type (UC-7).
func TemplateFor(contentType string) domain.ProductionTemplate {
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
