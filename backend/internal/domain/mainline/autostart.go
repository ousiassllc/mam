package mainline

// BudgetCeilingScope says what the budget ceiling does and does not stop: it counts the calls of the models whose price is known. A call whose price is
// unknown (a ChatGPT subscription, a cloud model without a price) is not counted, so it is not stopped by the ceiling, and these are the cases in which AUTO
// must still start (spec/spec.md §4.3). Every text that offers the ceiling as a brake says this with it.
const BudgetCeilingScope = "予算上限が数えるのは、単価が分かるモデルの呼び出しだけです。単価が不明の呼び出し（ChatGPT の定額枠、単価のないクラウドモデルなど）は数えず、上限を決めていても止まりません。"

// StopAuto is the sure way to stop AUTO from starting: the Project goes to DIRECTOR. There is no operation that cancels a generation that has started.
const StopAuto = "確実に止めるには、Project の MODE を DIRECTOR にしてください（まだ始めていない Shot は自動では始まりません。始めた生成は終わるまで続きます）。"

// AutoStartNotice is what the screens, the thread and the MCP tools say when AUTO is chosen: it is the consent to start the paid generation, so nothing is
// asked before it starts (spec/spec.md §4.3). It names the cost, the unknown price, what the budget ceiling can and cannot stop, and what stops it.
const AutoStartNotice = "AUTO を選ぶと、Scene / Shot の計画のあと、承認を待たずに Reference Pack → Shot 生成を自動で始めます。有料の生成が含まれ、費用がかかります。" +
	"単価が分からないモデルは「不明」と表示しますが、不明でも止めずに始めます。" + BudgetCeilingScope + StopAuto

// AutoStartAtAnyLevel is what the Confirmation Level control and the thread say: no level asks before the generation starts; the level decides the other checkpoints.
const AutoStartAtAnyLevel = "AUTO では、どの確認レベルでも生成の開始は確認しません。確認レベルが決めるのは脚本・Rough Cut・Final の確認です。"
