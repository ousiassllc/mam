package repair

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// escalate asks the user for a decision when the Retry Policy stops the loop: an Escalation card,
// posted at every Confirmation Level.
func (a *Agent) escalate(ctx context.Context, shot repository.ShotRef, takeID string, res domain.JudgeResult, verdict domain.RetryVerdict) error {
	text := fmt.Sprintf("Shot %d の Repair を止めました: %s。\n最新の Judge 結果（Take %s）: %s",
		shot.Shot.OrderIndex+1, verdict.Reason(), domain.ShortID(takeID), Summarize(res))
	options := []string{
		"設定で上限を引き上げてから、もう一度 Repair を実行する",
		"最も良い Take をそのまま採用する",
		"Shot の指示を手動で修正して生成し直す",
	}
	if err := a.Chat.RaiseEscalation(context.WithoutCancel(ctx), shot.ProjectID, text, options, shot.Shot.ID); err != nil {
		return err
	}
	if note, ok := ctx.Value(doneNoteKey{}).(*doneNote); ok {
		note.message = verdict.Reason() // announced by Handle once the Shot is released
	}
	return nil
}

// announceRegenerated tells the user a new Take was made by the repair, and that it is judged next.
func (a *Agent) announceRegenerated(ctx context.Context, st *step, action domain.RepairAction) {
	notice := domain.ChatMessage{
		Role: domain.RoleAssistant, Kind: domain.KindText,
		Content: fmt.Sprintf("Shot %d を %s で再生成しました（再試行 %d/%d 回目）。新しい Take を Judge します。",
			st.shot.Shot.OrderIndex+1, action.Label(), st.retries+1, st.eff.MaxRetries),
	}
	if err := a.Chat.Announce(context.WithoutCancel(ctx), st.shot.ProjectID, notice); err != nil {
		a.Logger.Error("post repair notice failed", "shot_id", st.shot.Shot.ID, "err", err)
	}
}

// announceEditLevel tells the user which ranges of the Take to fix in the edit (and that the Take
// goes into the Rough Cut as it is when it was selected). Edit-Level Repair makes no new Take, so
// there is nothing to judge again.
func (a *Agent) announceEditLevel(ctx context.Context, shot repository.ShotRef, takeID string, res domain.JudgeResult, adopted bool) error {
	var ranges []string
	for _, is := range res.Issues {
		ranges = append(ranges, fmt.Sprintf("%s（%s）", is.TimeRange, is.Type))
	}
	content := fmt.Sprintf("Shot %d の Take %s は再生成せず、編集で直します（Edit-Level Repair）。該当区間: %s",
		shot.Shot.OrderIndex+1, domain.ShortID(takeID), strings.Join(ranges, "、"))
	if adopted {
		content += "\nこの Take を採用し、Rough Cut に載せます。"
	}
	notice := domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindText, Content: content}
	return a.Chat.Announce(context.WithoutCancel(ctx), shot.ProjectID, notice)
}

// fail tells the user why the repair stopped and returns the error for the job record. The Shot is
// released (release) before the failure is announced: the client reads the Shot again on it.
func (a *Agent) fail(ctx context.Context, shot repository.ShotRef, err error, release func()) error {
	if ctx.Err() != nil {
		return err // shutting down: nobody is told
	}
	if errors.Is(err, repository.ErrNotFound) {
		return err
	}
	release()
	a.Logger.Error("repair failed", "shot_id", shot.Shot.ID, "err", err)
	reason := FailureReason(err)
	notice := domain.ChatMessage{
		Role: domain.RoleAssistant, Kind: domain.KindText,
		Content: fmt.Sprintf("Shot %d の Repair に失敗しました: %s\n対処したら、もう一度 Repair を実行してください。", shot.Shot.OrderIndex+1, reason),
	}
	if aerr := a.Chat.Announce(context.WithoutCancel(ctx), shot.ProjectID, notice); aerr != nil {
		a.Logger.Error("post repair failure notice failed", "shot_id", shot.Shot.ID, "err", aerr)
	}
	a.Notify(generation.Progress{ShotID: shot.Shot.ID, Action: generation.ActionRepair, Stage: generation.StageFailed, Message: reason})
	return errors.New(reason)
}

// FailureReason is the user-facing cause of a failed repair. Details stay in the log.
func FailureReason(err error) string {
	switch {
	case errors.Is(err, generation.ErrNoSpec):
		return "この Shot にはまだ Shot Spec がありません（先に生成してください）"
	case errors.Is(err, generation.ErrNoOtherModel):
		return "切り替えられる別の動画モデルがありません"
	case errors.Is(err, repository.ErrLocked):
		return "LOCK されている項目のため、Shot Spec を書き換えられません"
	}
	return generation.FailureReason(err)
}

// Summarize is a one-line account of a Judge result for the chat: the verdict and its Issues.
func Summarize(res domain.JudgeResult) string {
	verdict := "FAIL"
	if res.Result == domain.JudgePass {
		verdict = "PASS（最低スコア未達）"
	}
	var parts []string
	for _, is := range res.Issues {
		p := fmt.Sprintf("%s[%s]", is.Type, is.Severity)
		if is.LockViolation {
			p += "（LOCK 違反）"
		}
		parts = append(parts, p)
	}
	if len(parts) == 0 {
		return verdict
	}
	return verdict + " — " + strings.Join(parts, ", ")
}
