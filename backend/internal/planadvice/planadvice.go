// Package planadvice says what to do to go on when the planning job of a Project stopped (#1001). The failure text is the one the
// orchestrator wrote (agent/llm/failmsg words it); this advice is told next to it, never in its place. It is a package of its own
// because domain is at its line limit.
package planadvice

import (
	"fmt"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// noLLMKeyMarker is the phrase the reason of a plan job that stopped for want of an LLM API key carries (failmsg words it, the
// orchestrator stores it as the error of the job). The advice is found by it, so it does not depend on the rest of the wording,
// which names the Agent and the providers that were tried.
const noLLMKeyMarker = "API キーが設定されていません"

// draftMarker is the phrase of DraftNote: a plan job whose reason carries it stopped after Characters were drafted from the images attached to
// the Brief (#1022), which is how the advice finds out without asking the database.
const draftMarker = "登場人物の下書き"

// DraftNote is the sentence the orchestrator adds to the reason of the plan job that stopped for want of an LLM API key when the Project has
// n drafts of Characters made from the images attached to its Brief (n > 0; characterdraft). The advice of For then says what to do with them.
func DraftNote(n int) string {
	return fmt.Sprintf("Brief の添付画像から%s（draft）を %d 件、顔の参照画像つきで作りました（名前は仮で、画像から読み取れない項目は空です）。", draftMarker, n)
}

// For is the advice of a job: what to do to go on when it is a planning job that failed for want of an LLM API key (the Agents that
// write the plan cannot run, so the Project has no Characters and a Series made from it has an empty canon), "" for any other job. When
// the reason says Characters were drafted from the attached images (DraftNote), the second way is to correct those drafts.
// forMCP names the tools of an MCP client where the screen would name its buttons.
func For(j domain.Job, forMCP bool) string {
	if j.Kind != domain.JobPlan || j.Status != domain.JobFailed || !strings.Contains(j.Error, noLLMKeyMarker) {
		return ""
	}
	drafted := strings.Contains(j.Error, draftMarker)
	const choose = "続けるには、次のどちらかをしてください。"
	if forMCP {
		const llm = "(1) LLM を使えるようにする（get_global_settings で使うプロバイダーを確かめ、set_credential で API キーを入れるか、start_chatgpt_login で ChatGPT にログインする。" +
			"set_llm_assignment で割り当ても変えられる）。そのあと post_chat_message で何かメッセージを送ると、企画は続きから再開します。"
		if drafted {
			return choose + llm +
				"(2) LLM なしで進める。作ってある登場人物の下書き（get_world_state の characters で draft が true のもの）を、update_character で髪型・衣装・性格・声などを書き足す" +
				"（直すと draft は false になる）。assign_character_reference で顔・全身の参照画像も変えられ、足りない登場人物は create_character で作れる。"
		}
		return choose + llm +
			"(2) LLM なしで進める。create_character で登場人物（名前・髪型・衣装・性格・声と、Reference Library の face / body の画像）を直接作る。" +
			"project_id を渡せばこの Project に、series_id を渡せば Series の正本に作れる。返る character_id は assign_character_reference などにも使える。"
	}
	const llm = "(1) 設定の「API キーとログイン」で鍵を登録するか ChatGPT にログインし、「テキスト生成（LLM）」の割り当てを合わせる。そのあとチャットに何かメッセージを送ると、企画は続きから再開します。"
	if drafted {
		return choose + llm +
			"(2) LLM なしで進める。World State の登場人物のうち「下書き」の印がついたものの髪型・衣装・性格・声などを書き足す（書き足すと印は消える）。" +
			"顔・全身の参照画像も変えられ、足りない登場人物は「登場人物を追加」で作れる。"
	}
	return choose + llm +
		"(2) LLM なしで進める。World State の「登場人物を追加」、または Series の Characters の「登場人物を追加」から、登場人物を直接作る。"
}

// QueuedAfter is how long a plan job may be queued before the card says that it has not started (#1107) and the orchestrator logs it.
const QueuedAfter = 30 * time.Second

// Waiting is what the progress card of a planning job says when the job has been queued for QueuedAfter and has not started (every worker of the queue is busy with
// other jobs, or the queue is stopped): that nothing is wrong with the Project, why the job waits, and where to look. "" for any other job or a job that has not waited
// that long.
func Waiting(j domain.Job, now time.Time) string {
	if j.Kind != domain.JobPlan || j.Status != domain.JobQueued || j.CreatedAt.IsZero() || now.Sub(j.CreatedAt) < QueuedAfter {
		return ""
	}
	return fmt.Sprintf("企画・脚本の作成は、まだ始まっていません（受け付けてから %d 秒）。ほかの処理（生成・判定・モデルの導入など）が先に動いていると、空くまで待ちます。"+
		"サイドメニューの「ジョブ」で動いている処理を確かめられます。始まれば、このカードが「実行中」に変わります。", int(now.Sub(j.CreatedAt).Seconds()))
}
