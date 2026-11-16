package shotprompt

import "github.com/ousiassllc/mam/backend/internal/domain"

// PromptLimit is the number of characters the prompt of a video model that has no negative prompt field may have: the Constraints and the avoid list are part of the prompt, so
// Fit cuts the description to this length and keeps them whole.
func PromptLimit(m domain.ModelName) int {
	switch m {
	case domain.ModelKling4: // the prompt takes negative descriptions; English input up to 26,000 characters
		return 26000
	case domain.ModelMiniMaxH3, domain.ModelMiniMaxH3Max: // up to 7000 characters
		return 7000
	case domain.ModelWan3: // up to 20,000 characters (the API cuts the rest, which would lose the Constraints)
		return 20000
	case domain.ModelLumaRay32: // 1 to 6,000 characters
		return 6000
	case domain.ModelRunwayGen45: // a 1000 character limit: Constraints and the avoid list are kept whole
		return 1000
	}
	return 2400
}
