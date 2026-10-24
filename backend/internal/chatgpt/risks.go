package chatgpt

// Links the sign-in UI is required to show (SIWC ui-ux-guidelines, accounts-and-sessions "Tracking usage").
const (
	// UsageURL is where the user reviews and limits what apps use of the ChatGPT plan.
	UsageURL = "https://chatgpt.com/settings/usage"
	// ContinueLabel is the label of the sign-in button.
	ContinueLabel = "Continue with ChatGPT"
	// UsingPlanText is shown near the model selector while a model of the ChatGPT plan is selected.
	UsingPlanText = "Using ChatGPT plan"
	// ManageUsageText is the label of the UsageURL link.
	ManageUsageText = "Manage usage"
)

// ImageRisks are the risks of the unofficial image generation. The settings screen and the MCP tool show them in full
// before it is turned on, and turning it on requires the user to confirm them.
var ImageRisks = []string{
	"OpenAI の公開 API ではない、非公式の経路（chatgpt.com の内部 API）を使います。予告なく使えなくなることがあります。",
	"OpenAI の公式の案内（ChatGPT plan usage のプレビューの制限）では、画像生成はサポート外とされています。",
	"画像を作るたびに、ChatGPT のプランの利用上限を消費します。",
}

// ImageRisksSummary is ImageRisks in one text.
func ImageRisksSummary() string {
	out := ""
	for i, r := range ImageRisks {
		if i > 0 {
			out += " "
		}
		out += r
	}
	return out
}
