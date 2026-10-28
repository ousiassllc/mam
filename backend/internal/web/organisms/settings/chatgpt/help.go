package chatgpt

// The explanation of each login system, behind the "?" of its card. The panels are drawn by Section, outside the
// polled #chatgpt-panel (the card has only the button: settingsui.Card.HelpPanelElsewhere), so that a poll that swaps
// the cards does not close a help that is open.
const (
	officialHelp = "個人（Go / Plus / Pro）の ChatGPT のプランで、テキスト生成を使います。"
	imageHelp    = "画像生成だけに使う、非公式の経路です。既定はオフで、オンにするときにリスクの確認が要ります。"
	codexHelp    = "Codex の app-server（OpenAI の公式のプログラム）をこの PC で動かし、そこで ChatGPT にログインします。Business / Enterprise / Edu のワークスペースのアカウントでも、個人のアカウントでも使えます。画像・動画・音声の生成には使いません。LLM の割り当てでは、プロバイダー「chatgpt-codex」を選びます。"
)

// cardHelp is the help of the card of one login system.
type cardHelp struct{ kind, title, text string }

// cardHelps are the helps of the cards, in the order of the cards.
var cardHelps = []cardHelp{
	{officialKind, officialTitle, officialHelp},
	{imageKind, imageTitle, imageHelp},
	{codexKind, codexTitle, codexHelp},
}
