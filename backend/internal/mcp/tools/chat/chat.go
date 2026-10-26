// Package chat holds the MCP tools of the chat thread of a Project (the Chat Workspace): reading the
// thread (and following it: the screen's WebSocket /ws/projects/:id only says "there is something new", the
// tools poll with a cursor), posting a message with attachments, and answering an Approval card.
package chat

import (
	"context"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/stage"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Register adds the tools and the upload purpose of this package.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	files := stage.New()
	registerPurpose(r.Files, svc, files)

	mcpkit.Read(r, "list_chat_messages",
		"プロジェクトのチャットのメッセージ・カードを時系列で返す（Chat Workspace と同じ内容）。after に直前の last_message_id を渡すとそれ以降だけ返す（進捗の確認はこのポーリング）/ List the chat thread; pass the previous last_message_id as after to get only what is new.",
		listMessages(svc.Chat))
	mcpkit.Read(r, "get_chat_message",
		"チャットのカード 1 件の今の状態（承認・質問・進捗カードの更新を取り直す）/ One card as it is now.",
		getMessage(svc.Chat))
	mcpkit.Write(r, "post_chat_message",
		"チャットに発言する。Brief の質問への回答・自由な指示・DIRECTOR MODE の指示になり、AI の返答も同じ結果に入る。ファイルは create_upload_ticket（purpose=chat_attachment）で上げてから attachment_upload_ids で渡す / Post a message; attach files uploaded with purpose chat_attachment.",
		postMessage(svc.Chat, files))
	mcpkit.Write(r, "respond_approval",
		"承認カードに答える（approve または request_changes）。カードは 1 回だけ答えられ、答えると次の工程へ進む / Answer an Approval card.",
		respondApproval(svc.Chat))
}

// ListInput names the Project and, to follow the thread, the last message already seen.
type ListInput struct {
	ProjectID string `json:"project_id"`
	After     string `json:"after,omitempty" jsonschema:"このメッセージ ID より後だけ返す。省略すると全件"`
}

// ThreadOutput is messages of a thread with the Project state.
type ThreadOutput struct {
	Project  Project   `json:"project"`
	Messages []Message `json:"messages"`
	// Updated are earlier cards that the returned messages answered; replace them in what you hold.
	Updated       []Message `json:"updated" jsonschema:"返したメッセージが答えた、以前の質問・承認カード（最新の状態）"`
	LastMessageID string    `json:"last_message_id" jsonschema:"次の after に渡す ID。新しいメッセージがなければ渡した after のまま"`
}

func threadOf(ws service.Workspace, after string) ThreadOutput {
	out := ThreadOutput{Project: projectOf(ws.Project), Messages: messagesOf(ws.Messages), Updated: messagesOf(ws.Updated), LastMessageID: after}
	if n := len(out.Messages); n > 0 {
		out.LastMessageID = out.Messages[n-1].ID
	}
	return out
}

func listMessages(svc *service.ChatService) mcpkit.Handler[ListInput, ThreadOutput] {
	return func(ctx context.Context, in ListInput) (ThreadOutput, error) {
		ws, err := svc.Messages(ctx, in.ProjectID, in.After)
		if err != nil {
			return ThreadOutput{}, err
		}
		return threadOf(ws, in.After), nil
	}
}

// MessageInput names one message.
type MessageInput struct {
	ProjectID string `json:"project_id"`
	MessageID string `json:"message_id"`
}

// MessageOutput is one card with the Project state.
type MessageOutput struct {
	Project Project `json:"project"`
	Message Message `json:"message"`
}

func getMessage(svc *service.ChatService) mcpkit.Handler[MessageInput, MessageOutput] {
	return func(ctx context.Context, in MessageInput) (MessageOutput, error) {
		project, m, err := svc.Card(ctx, in.ProjectID, in.MessageID)
		if err != nil {
			return MessageOutput{}, err
		}
		return MessageOutput{Project: projectOf(project), Message: messageOf(m)}, nil
	}
}

// PostInput is one user message.
type PostInput struct {
	ProjectID           string   `json:"project_id"`
	Content             string   `json:"content,omitempty" jsonschema:"発言（4000 文字まで）。添付だけのときは省略できる"`
	ReplyToMessageID    string   `json:"reply_to_message_id,omitempty" jsonschema:"答える質問・相談・承認カードの ID（省略すると自由入力）"`
	AttachmentAssetIDs  []string `json:"attachment_asset_ids,omitempty" jsonschema:"このプロジェクトの既存アセットの ID（添付の再利用）"`
	AttachmentUploadIDs []string `json:"attachment_upload_ids,omitempty" jsonschema:"chat_attachment で上げたファイルの upload_id。添付は既存アセットと合わせて 8 件まで"`
}

func postMessage(svc *service.ChatService, files *stage.Store) mcpkit.Handler[PostInput, ThreadOutput] {
	return func(ctx context.Context, in PostInput) (ThreadOutput, error) {
		staged, err := files.Claim(in.ProjectID, in.AttachmentUploadIDs)
		if err != nil {
			return ThreadOutput{}, err
		}
		ws, err := svc.PostMessage(ctx, in.ProjectID, service.PostMessageInput{
			Content: in.Content, ReplyToMessageID: in.ReplyToMessageID,
			AttachmentAssetIDs: slices.Clone(in.AttachmentAssetIDs), Uploads: stage.Uploads(staged),
		})
		if err != nil {
			files.Release(staged)
			return ThreadOutput{}, err
		}
		files.Discard(staged)
		return threadOf(ws, ""), nil
	}
}

// ApprovalInput answers one Approval card.
type ApprovalInput struct {
	ProjectID string `json:"project_id"`
	MessageID string `json:"message_id" jsonschema:"承認カードのメッセージ ID"`
	Decision  string `json:"decision" jsonschema:"approve or request_changes"`
	Comment   string `json:"comment,omitempty" jsonschema:"任意のコメント（修正の依頼内容など。4000 文字まで）"`
}

func respondApproval(svc *service.ChatService) mcpkit.Handler[ApprovalInput, MessageOutput] {
	return func(ctx context.Context, in ApprovalInput) (MessageOutput, error) {
		project, card, err := svc.RespondApproval(ctx, in.ProjectID, in.MessageID, service.Decision(in.Decision), in.Comment)
		if err != nil {
			return MessageOutput{}, err
		}
		return MessageOutput{Project: projectOf(project), Message: messageOf(card)}, nil
	}
}
