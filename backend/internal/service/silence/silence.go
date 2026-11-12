// Package silence keeps the thread of a Project from ending with the words of the user alone (#1107): a message
// that is stored is followed by the AI's answer, a Progress card, or a notice that says why nothing was started.
package silence

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/llm/failmsg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Guard is what answers the message that nothing answered.
type Guard struct {
	Chat     *repository.ChatRepository
	Projects *repository.ProjectRepository
	Logger   *slog.Logger
	// PipelineOn reports that the AUTO pipeline is wired (it needs the job queue and a Reasoning LLM).
	PipelineOn bool
	// Running reports that a plan job of the Project is queued or running (nil: never). A message that arrives meanwhile starts no job
	// and no Progress card, the job on the Project serves it.
	Running func(projectID string) bool
}

// Answer returns the messages it recorded after msgs, what the turn stored (the user's message among
// them). kickErr is why the pipeline could not be started ("" when it was). Each case is logged with the
// project_id and the message_id of the user's message, so that it is found through the application log.
func (g Guard) Answer(ctx context.Context, projectID string, msgs []domain.ChatMessage, kickErr error) ([]domain.ChatMessage, error) {
	i := slices.IndexFunc(msgs, func(m domain.ChatMessage) bool { return m.Role == domain.RoleUser })
	if i < 0 {
		return nil, nil
	}
	user := msgs[i]
	log := g.Logger.With("project_id", projectID, "message_id", user.ID)
	if kickErr != nil {
		log.Error("start the AUTO pipeline after a message failed", "err", kickErr)
		return g.post(ctx, projectID, text("メッセージは保存しましたが、制作を始められませんでした: "+generation.FailureReason(kickErr)+"\nもう一度メッセージを送ると、続きから再開を試みます。"))
	}
	if slices.ContainsFunc(msgs, func(m domain.ChatMessage) bool { return m.Role == domain.RoleAssistant }) {
		return nil, nil
	}
	// Nothing in the turn answers: a Progress card the pipeline posted since would.
	later, err := g.Chat.List(ctx, projectID, user.ID)
	if err != nil {
		log.Error("read the thread after a message failed", "err", err) // unknown: a notice too many is better than none
	} else if len(later) > 0 {
		return nil, nil
	}
	if g.Running != nil && g.Running(projectID) {
		log.Info("a message was stored while a plan job is on the project; told in the thread")
		m := text("メッセージは保存しました。制作の処理がすでに進行中のため、新しい処理は始めていません。上の進捗カードで状況を確認できます。失敗した場合は、ここに理由を伝えます。")
		return g.post(ctx, projectID, m)
	}
	project, err := g.Projects.Get(ctx, projectID)
	if err != nil {
		return nil, err
	}
	brief, err := g.Projects.GetBrief(ctx, projectID)
	if err != nil {
		return nil, err
	}
	log.Warn("a message was stored with no answer, progress or failure; told in the thread",
		"state", project.State, "mode", project.Mode, "pipeline", g.PipelineOn)
	return g.post(ctx, projectID, Notice(project, g.PipelineOn, briefing.ReadyToPlan(brief, project.ConfirmationLevel)))
}

// Recover is deferred by a handler of a message already stored: a panic is logged with the project_id and the stack, answered in the thread
// (report records the failure and returns it) and returned as the error of the handler, so that the request does not end with the user's words alone.
func Recover(logger *slog.Logger, projectID string, report func(error) error, err *error) {
	if r := recover(); r != nil {
		logger.Error("handling a message panicked", "project_id", projectID, "panic", r, "stack", string(debug.Stack()))
		*err = report(fmt.Errorf("handling a message panicked: %v", r))
	}
}

// post records the AI message whatever happened to the request since.
func (g Guard) post(ctx context.Context, projectID string, m domain.ChatMessage) ([]domain.ChatMessage, error) {
	m.ID = domain.NewID()
	msgs := []domain.ChatMessage{m}
	return msgs, g.Projects.Apply(context.WithoutCancel(ctx), repository.Turn{ProjectID: projectID, Messages: msgs})
}

func text(content string) domain.ChatMessage {
	return domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindText, Content: content}
}

// Notice tells why a message of the user starts nothing: what the Project waits for, or which part of the
// setup is missing. briefComplete is whether PLANNING may start (briefing.ReadyToPlan). It names the state the Project is in, never a model.
func Notice(p domain.Project, pipelineOn, briefComplete bool) domain.ChatMessage {
	driven := p.State == domain.StatePlanning || p.State == domain.StateVisualDesign || (p.State == domain.StateDraft && briefComplete)
	switch {
	case p.Mode == domain.ModeAuto && driven && !pipelineOn:
		m := text("メッセージは保存しましたが、自動制作を動かす準備ができていないため、続きを始められません（テキスト生成の LLM が割り当てられていないか、ジョブキューが動いていません）。設定を整えてからメッセージを送ると、続きから再開します。")
		m.RefType, m.RefID = domain.RefGlobalSettings, failmsg.SettingsSlug
		return m
	case p.State == domain.StateAwaitingApproval:
		return text("メッセージは保存しました。いまは承認待ちです。承認カードで承認または修正依頼を伝えると、続きから進みます。")
	case p.State == domain.StateDraft && !briefComplete:
		return text("メッセージは保存しましたが、Brief の項目として読み取れる内容がありませんでした（これまでの Brief は変えていません）。質問カードか「この内容でよいですか？」のカードに返信するか、Brief の画面から入力・確定してください。")
	}
	hint := "Shot や Take への指示は、DIRECTOR モードに切り替えてから送ってください。"
	if p.Mode == domain.ModeDirector {
		hint = "指示は、Brief が完成して制作が始まってから受け付けます。"
	}
	return text(fmt.Sprintf("メッセージは保存しましたが、現在の状態（%s・%s モード）では、このメッセージに対して自動で行う処理はありません。%s", p.State, p.Mode, hint))
}
