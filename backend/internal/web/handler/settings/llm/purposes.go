package llm

// purposes is what each Agent is for, in one line (spec/architecture/model-adapters.md §2). The two Agents no code calls say so.
var purposes = map[string]string{
	"orchestrator":        "状態機械の進行管理（今は LLM を呼びません）",
	"producer":            "Creative Brief からコンセプトを展開する",
	"writer":              "コンセプトから脚本を書く",
	"director":            "Scene / Shot の構成を計画する",
	"storyboard":          "Shot ごとの絵コンテを作る",
	"art_director":        "参照画像を解析して Reference Pack を組む",
	"prompt_planner":      "Shot Spec と Prompt の候補を作る",
	"video_judge":         "生成した動画のフレームを評価する",
	"repair":              "Judge の指摘から修復の戦略を選ぶ",
	"editor":              "Rough Cut のタイムラインを判定する",
	"audio_director":      "Scene の感情から音声を設計する",
	"command_interpreter": "DIRECTOR 指示を構造化した操作に変える",
	"state_extractor":     "World State の差分を抽出する",
	"final_qc":            "最終検査（今は LLM を呼びません）",
}
