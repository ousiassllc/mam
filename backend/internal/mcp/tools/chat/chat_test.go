package chat_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/chat"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/stage/stagetest"
	"github.com/ousiassllc/mam/backend/internal/service"
)

func newEnv(t *testing.T) *mcptest.Env {
	return mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) { chat.Register(r, env.Services) })
}

func createProject(t *testing.T, env *mcptest.Env) domain.Project {
	t.Helper()
	p, err := env.Services.Projects.Create(context.Background(), service.CreateProjectInput{Content: "美容液の縦型15秒CMを作りたい。上品な雰囲気で"})
	require.NoError(t, err)
	return p
}

func list(t *testing.T, env *mcptest.Env, projectID, after string) chat.ThreadOutput {
	t.Helper()
	var out chat.ThreadOutput
	env.CallOK("list_chat_messages", map[string]any{"project_id": projectID, "after": after}, &out)
	return out
}

func TestListChatMessages_ReadsTheThreadAndFollowsItWithACursor(t *testing.T) {
	env := newEnv(t)
	p := createProject(t, env)

	all := list(t, env, p.ID, "")
	require.Len(t, all.Messages, 2)
	assert.Equal(t, "user", all.Messages[0].Role)
	q := all.Messages[1]
	assert.Equal(t, "question", q.Kind)
	assert.Equal(t, "objective", q.RefID, "the first item the extraction left open")
	assert.True(t, q.Open)
	assert.True(t, q.Pending)
	assert.NotEmpty(t, q.Options, "the quick answers are returned decoded, not as the stored JSON")
	assert.NotContains(t, q.Text, `"options"`)
	assert.Equal(t, q.ID, all.LastMessageID)
	assert.Equal(t, "DRAFT", all.Project.State)

	idle := list(t, env, p.ID, all.LastMessageID)
	assert.Empty(t, idle.Messages)
	assert.Equal(t, all.LastMessageID, idle.LastMessageID, "nothing new: the cursor stays")

	var posted chat.ThreadOutput
	env.CallOK("post_chat_message", map[string]any{"project_id": p.ID, "content": "Instagram広告", "reply_to_message_id": q.ID}, &posted)
	fresh := list(t, env, p.ID, all.LastMessageID)
	require.NotEmpty(t, fresh.Messages)
	assert.Equal(t, fresh.Messages[0].ID, posted.Messages[0].ID, "only what is after the cursor")
	assert.Equal(t, fresh.LastMessageID, posted.LastMessageID)
	require.Len(t, fresh.Updated, 1, "the answered question is returned again, as it is now")
	assert.Equal(t, q.ID, fresh.Updated[0].ID)
	assert.False(t, fresh.Updated[0].Open)
}

func TestListChatMessages_UnknownProjectIsNotFound(t *testing.T) {
	env := newEnv(t)
	assert.Contains(t, env.CallError("list_chat_messages", map[string]any{"project_id": "nope"}), "not_found")
	assert.Contains(t, env.CallError("get_chat_message", map[string]any{"project_id": "nope", "message_id": "m"}), "not_found")

	p := createProject(t, env)
	assert.Contains(t, env.CallError("get_chat_message", map[string]any{"project_id": p.ID, "message_id": "nope"}), "not_found")
}

func TestGetChatMessage_ReturnsTheCardAsItIsNow(t *testing.T) {
	env := newEnv(t)
	p := createProject(t, env)
	q := list(t, env, p.ID, "").Messages[1]

	var before chat.MessageOutput
	env.CallOK("get_chat_message", map[string]any{"project_id": p.ID, "message_id": q.ID}, &before)
	assert.True(t, before.Message.Open)

	env.CallOK("post_chat_message", map[string]any{"project_id": p.ID, "content": "AIに任せる", "reply_to_message_id": q.ID}, nil)
	var after chat.MessageOutput
	env.CallOK("get_chat_message", map[string]any{"project_id": p.ID, "message_id": q.ID}, &after)
	assert.False(t, after.Message.Open, "the card changed in place")
}

func TestPostChatMessage_ValidatesLikeTheScreen(t *testing.T) {
	env := newEnv(t)
	p := createProject(t, env)

	assert.Contains(t, env.CallError("post_chat_message", map[string]any{"project_id": p.ID}), "validation: invalid input: content")
	long := make([]rune, domain.MaxMessageRunes+1)
	for i := range long {
		long[i] = 'あ'
	}
	assert.Contains(t, env.CallError("post_chat_message", map[string]any{"project_id": p.ID, "content": string(long)}), "validation:")
	assert.Contains(t, env.CallError("post_chat_message", map[string]any{"project_id": p.ID, "content": "x", "reply_to_message_id": "nope"}), "reply_to_message_id")
	assert.Contains(t, env.CallError("post_chat_message", map[string]any{"project_id": p.ID, "content": "x", "attachment_asset_ids": []string{"nope"}}), "attachment_asset_ids")
	assert.Contains(t, env.CallError("post_chat_message", map[string]any{"project_id": "nope", "content": "x"}), "not_found")
}

func TestPostChatMessage_AnAnsweredQuestionIsAConflict(t *testing.T) {
	env := newEnv(t)
	p := createProject(t, env)
	q := list(t, env, p.ID, "").Messages[1]
	args := map[string]any{"project_id": p.ID, "content": "Instagram広告", "reply_to_message_id": q.ID}
	env.CallOK("post_chat_message", args, nil)
	assert.Contains(t, env.CallError("post_chat_message", args), "conflict")
}

func TestPostChatMessage_AttachmentsAreUploadedFirstAndBelongToOneProject(t *testing.T) {
	env := newEnv(t)
	p, other := createProject(t, env), createProject(t, env)

	status, staged := stagetest.Upload(env, chat.PurposeChatAttachment, map[string]string{"project_id": p.ID}, "product.png", stagetest.PNG)
	require.Equal(t, http.StatusCreated, status)
	uploadID, _ := staged["upload_id"].(string)
	require.NotEmpty(t, uploadID)

	assert.Contains(t, env.CallError("post_chat_message", map[string]any{"project_id": other.ID, "attachment_upload_ids": []string{uploadID}}),
		"validation: invalid input: attachment_upload_ids", "the file was uploaded for p, not for the other Project")

	var out chat.ThreadOutput
	env.CallOK("post_chat_message", map[string]any{"project_id": p.ID, "content": "これが商品", "attachment_upload_ids": []string{uploadID}}, &out)
	user := out.Messages[0]
	require.Len(t, user.Attachments, 1)
	assert.Equal(t, "image", user.Attachments[0].Type)
	assert.Equal(t, "image/png", user.Attachments[0].MimeType)
	file, err := env.Services.Projects.Asset(context.Background(), p.ID, user.Attachments[0].AssetID)
	require.NoError(t, err, "the file became an asset of the Project")
	assert.FileExists(t, file.Path)
	view, err := env.Services.Projects.Brief(context.Background(), p.ID)
	require.NoError(t, err)
	assert.Contains(t, view.Brief.Assets, user.Attachments[0].AssetID, "an image joins the Brief like an attachment of the screen")

	assert.Contains(t, env.CallError("post_chat_message", map[string]any{"project_id": p.ID, "attachment_upload_ids": []string{uploadID}}),
		"attachment_upload_ids", "an upload works once")
}

func TestPostChatMessage_AFileTheServiceRefusesStaysUsableAfterTheError(t *testing.T) {
	env := newEnv(t)
	p := createProject(t, env)
	status, staged := stagetest.Upload(env, chat.PurposeChatAttachment, map[string]string{"project_id": p.ID}, "fake.png", []byte("not a png at all"))
	require.Equal(t, http.StatusCreated, status)
	uploadID := staged["upload_id"].(string)

	args := map[string]any{"project_id": p.ID, "content": "x", "attachment_upload_ids": []string{uploadID}}
	first := env.CallError("post_chat_message", args)
	assert.Contains(t, first, "拡張子とファイルの内容が一致しません")
	assert.Equal(t, first, env.CallError("post_chat_message", args), "the same refusal, not 'upload not found': the file was put back")
	assert.Empty(t, list(t, env, p.ID, "").Messages[2:], "nothing was posted")
}

func TestChatAttachmentTicket_IsBoundToAnExistingProject(t *testing.T) {
	env := newEnv(t)
	status, _ := stagetest.Upload(env, chat.PurposeChatAttachment, map[string]string{"project_id": "nope"}, "a.png", stagetest.PNG)
	assert.Equal(t, http.StatusNotFound, status)
}

func requestApproval(t *testing.T, env *mcptest.Env, p domain.Project) chat.Message {
	t.Helper()
	posted, err := env.Services.Chat.RequestApproval(context.Background(), p.ID, domain.CheckpointScript, "脚本 v1", "sv1")
	require.NoError(t, err)
	require.True(t, posted)
	msgs := list(t, env, p.ID, "").Messages
	card := msgs[len(msgs)-1]
	require.Equal(t, "approval", card.Kind)
	return card
}

func TestRespondApproval_AnswersOnceAndMovesTheProjectOn(t *testing.T) {
	env := newEnv(t)
	p := createProject(t, env)
	card := requestApproval(t, env, p)
	assert.Equal(t, "pending", card.ApprovalStatus)
	assert.Equal(t, "AWAITING_APPROVAL", list(t, env, p.ID, "").Project.State)

	var out chat.MessageOutput
	env.CallOK("respond_approval", map[string]any{"project_id": p.ID, "message_id": card.ID, "decision": "approve", "comment": "いいですね"}, &out)
	assert.Equal(t, "approved", out.Message.ApprovalStatus)
	assert.NotEqual(t, "AWAITING_APPROVAL", out.Project.State)

	assert.Contains(t, env.CallError("respond_approval", map[string]any{"project_id": p.ID, "message_id": card.ID, "decision": "approve"}), "conflict")
	var answered bool
	for _, m := range list(t, env, p.ID, "").Messages {
		answered = answered || m.Role == "user" && m.Text == "承認しました。いいですね" && m.ReplyToMessageID == card.ID
	}
	assert.True(t, answered, "the answer is in the thread, as a reply to the card")
}

func TestRespondApproval_RequestChangesKeepsTheComment(t *testing.T) {
	env := newEnv(t)
	p := createProject(t, env)
	card := requestApproval(t, env, p)

	var out chat.MessageOutput
	env.CallOK("respond_approval", map[string]any{"project_id": p.ID, "message_id": card.ID, "decision": "request_changes", "comment": "もっと短く"}, &out)
	assert.Equal(t, "changes_requested", out.Message.ApprovalStatus)
	var found bool
	for _, m := range list(t, env, p.ID, "").Messages {
		found = found || m.Role == "user" && m.Text == "修正を依頼: もっと短く"
	}
	assert.True(t, found)
}

func TestRespondApproval_Validation(t *testing.T) {
	env := newEnv(t)
	p := createProject(t, env)
	card := requestApproval(t, env, p)
	question := list(t, env, p.ID, "").Messages[1]

	assert.Contains(t, env.CallError("respond_approval", map[string]any{"project_id": p.ID, "message_id": card.ID, "decision": "maybe"}), "validation: invalid input: decision")
	assert.Contains(t, env.CallError("respond_approval", map[string]any{"project_id": p.ID, "message_id": question.ID, "decision": "approve"}), "validation: invalid input: message")
	assert.Contains(t, env.CallError("respond_approval", map[string]any{"project_id": p.ID, "message_id": "nope", "decision": "approve"}), "not_found")
	assert.Contains(t, env.CallError("respond_approval", map[string]any{"project_id": "nope", "message_id": card.ID, "decision": "approve"}), "not_found")
	var after chat.MessageOutput
	env.CallOK("get_chat_message", map[string]any{"project_id": p.ID, "message_id": card.ID}, &after)
	assert.Equal(t, "pending", after.Message.ApprovalStatus, "refused answers leave the card as it was")
}
