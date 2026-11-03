// Package failmsg words the failures of the Reasoning LLM Adapter for the user: the names of the Agents and the message of a
// call that failed for want of an API key (#1006). It is a package of its own, over package llm, so that llm stays within its size.
package failmsg

import "github.com/ousiassllc/mam/backend/internal/agent/llm"

// labels are the Japanese names of the Agents, as the settings screen and the failure messages call them.
var labels = map[llm.Agent]string{
	llm.AgentOrchestrator:       "Orchestrator（進行管理）",
	llm.AgentProducer:           "Producer（コンセプト）",
	llm.AgentWriter:             "Writer（脚本）",
	llm.AgentDirector:           "Director（Scene / Shot の計画）",
	llm.AgentStoryboard:         "Storyboard（絵コンテ）",
	llm.AgentArtDirector:        "Art Director（Reference Pack）",
	llm.AgentPromptPlanner:      "Prompt Planner（Shot Spec）",
	llm.AgentVideoJudge:         "AI Video Judge（動画の評価）",
	llm.AgentRepair:             "Repair（修復戦略の選択）",
	llm.AgentEditor:             "Editor（編集の判定）",
	llm.AgentAudioDirector:      "Audio Director（音声の設計）",
	llm.AgentCommandInterpreter: "Command Interpreter（DIRECTOR 指示）",
	llm.AgentStateExtractor:     "State Extractor（World State の抽出）",
	llm.AgentFinalQC:            "Final QC（最終検査）",
}

// Label is the Japanese name of the Agent ("Producer（コンセプト）"); an Agent that is none of Agents() is its own name.
func Label(a llm.Agent) string {
	if l, ok := labels[a]; ok {
		return l
	}
	return string(a)
}
