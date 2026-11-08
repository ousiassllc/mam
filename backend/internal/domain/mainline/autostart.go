package mainline

// AutoStartNotice is what the screens, the thread and the MCP tools say when AUTO is chosen: it is the consent to start the paid generation, so nothing is
// asked before it starts (spec/spec.md §4.3). It names the cost, the unknown price and what stops it.
const AutoStartNotice = "AUTO を選ぶと、Scene / Shot の計画のあと、承認を待たずに Reference Pack → Shot 生成を自動で始めます。有料の生成が含まれ、費用がかかります。" +
	"単価が分からないモデルは「不明」と表示しますが、不明でも止めずに始めます。止めるには、設定で予算上限を決める（超えるなら始めず、相談します）か、MODE を DIRECTOR にしてください。"

// AutoStartAtAnyLevel is what the Confirmation Level control and the thread say: no level asks before the generation starts; the level decides the other checkpoints.
const AutoStartAtAnyLevel = "AUTO では、どの確認レベルでも生成の開始は確認しません。確認レベルが決めるのは脚本・Rough Cut・Final の確認です。"
