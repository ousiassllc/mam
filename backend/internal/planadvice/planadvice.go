// Package planadvice says what to do to go on when the planning job of a Project stopped (#1001). The failure text is the one the
// orchestrator wrote (agent/llm/failmsg words it); this advice is told next to it, never in its place. It is a package of its own
// because domain is at its line limit.
package planadvice

import (
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// noLLMKeyMarker is the phrase the reason of a plan job that stopped for want of an LLM API key carries (failmsg words it, the
// orchestrator stores it as the error of the job). The advice is found by it, so it does not depend on the rest of the wording,
// which names the Agent and the providers that were tried.
const noLLMKeyMarker = "API キーが設定されていません"

// For is the advice of a job: what to do to go on when it is a planning job that failed for want of an LLM API key (the Agents that
// write the plan cannot run, so the Project has no Characters and a Series made from it has an empty canon), "" for any other job.
// forMCP names the tools of an MCP client where the screen would name its buttons.
func For(j domain.Job, forMCP bool) string {
	if j.Kind != domain.JobPlan || j.Status != domain.JobFailed || !strings.Contains(j.Error, noLLMKeyMarker) {
		return ""
	}
	const choose = "続けるには、次のどちらかをしてください。"
	if forMCP {
		return choose +
			"(1) LLM を使えるようにする（get_global_settings で使うプロバイダーを確かめ、set_credential で API キーを入れるか、start_chatgpt_login で ChatGPT にログインする。" +
			"set_llm_assignment で割り当ても変えられる）。そのあと post_chat_message で何かメッセージを送ると、企画は続きから再開します。" +
			"(2) LLM なしで進める。create_character で登場人物（名前・髪型・衣装・性格・声と、Reference Library の face / body の画像）を直接作る。" +
			"project_id を渡せばこの Project に、series_id を渡せば Series の正本に作れる。返る character_id は assign_character_reference などにも使える。"
	}
	return choose +
		"(1) 設定の「API キーとログイン」で鍵を登録するか ChatGPT にログインし、「テキスト生成（LLM）」の割り当てを合わせる。そのあとチャットに何かメッセージを送ると、企画は続きから再開します。" +
		"(2) LLM なしで進める。World State の「登場人物を追加」、または Series の Characters の「登場人物を追加」から、登場人物を直接作る。"
}
