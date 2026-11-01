package router_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func TestApprovalCard_AnsweredOnceProjectWaitsUntilThen(t *testing.T) {
	a := newApp(t)
	ctx := context.Background()
	id := a.createProject("動画")
	posted, err := a.svc.Chat.RequestApproval(ctx, id, domain.CheckpointScript, "脚本 v1（3 Scene / 15秒）", "sv1")
	require.NoError(t, err)
	require.True(t, posted)

	body := a.get("/projects/"+id, false).Body.String()
	assert.Contains(t, body, `data-kind="approval"`)
	assert.Contains(t, body, "脚本 v1（3 Scene / 15秒）")
	assert.Contains(t, body, ">承認<")
	assert.Contains(t, body, ">修正を依頼<")
	assert.Contains(t, body, "確認待ち")
	assert.Contains(t, body, `data-state="AWAITING_APPROVAL"`, "the project waits while the card is unanswered")
	assert.Contains(t, body, `data-card-pending="true"`, "the client refetches an unanswered card after a reconnect")
	card := lastID(t, a.get("/projects/"+id+"/messages", true).Body.String()) // the newest message is the card

	rec := a.form(http.MethodPost, "/projects/"+id+"/approvals/"+card, url.Values{"decision": {"approve"}, "comment": {"OK"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "承認済み")
	assert.NotContains(t, rec.Body.String(), "data-card-pending", "an answered card is final")
	assert.NotContains(t, rec.Body.String(), ">修正を依頼<", "an answered card has no buttons")
	assert.Contains(t, rec.Body.String(), `data-state="VISUAL_DESIGN"`, "the badge shows the new state")
	assert.Equal(t, "chat:refresh", rec.Header().Get("HX-Trigger"), "the thread fetches the follow-up messages")

	again := a.form(http.MethodPost, "/projects/"+id+"/approvals/"+card, url.Values{"decision": {"request_changes"}})
	assert.Equal(t, http.StatusConflict, again.Code)
	assert.Equal(t, "none", again.Header().Get("HX-Reswap"))

	bad := a.form(http.MethodPost, "/projects/"+id+"/approvals/"+card, url.Values{"decision": {"later"}})
	assert.Equal(t, http.StatusUnprocessableEntity, bad.Code)
	implicit := a.form(http.MethodPost, "/projects/"+id+"/approvals/"+card, url.Values{"comment": {"ここを直して"}})
	assert.Equal(t, http.StatusUnprocessableEntity, implicit.Code, "a submit without a pressed button is not an approval")
	missing := a.form(http.MethodPost, "/projects/"+id+"/approvals/missing", url.Values{"decision": {"approve"}})
	assert.Equal(t, http.StatusNotFound, missing.Code)
}

func TestApprovalCard_RequestChangesShowsItsOwnStatus(t *testing.T) {
	a := newApp(t)
	id := a.createProject("動画")
	_, err := a.svc.Chat.RequestApproval(context.Background(), id, domain.CheckpointScript, "脚本 v1", "sv1")
	require.NoError(t, err)
	card := lastID(t, a.get("/projects/"+id+"/messages", true).Body.String())

	rec := a.form(http.MethodPost, "/projects/"+id+"/approvals/"+card, url.Values{"decision": {"request_changes"}, "comment": {"短く"}})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "修正依頼済み")
	assert.Contains(t, a.get("/projects/"+id+"/messages", true).Body.String(), "修正を依頼: 短く")
}

func TestComposer_HasModeAndQualityControlsAndStaysEnabledWhileGenerating(t *testing.T) {
	a := newApp(t)
	id := a.createProject("動画")
	a.announceProgress(id, "Scene 02 を生成中", domain.JobRunning)

	body := a.get("/projects/"+id, false).Body.String()
	assert.Contains(t, body, `name="mode"`)
	assert.Contains(t, body, `name="quality_policy"`)
	assert.Contains(t, body, `hx-put="/projects/`+id+`/quality-policy"`, "the Quality control switches the policy")
	assertChecked(t, body, "auto")
	assert.Contains(t, body, `value="balanced" selected`)
	assert.Contains(t, body, "Scene 02 を生成中")
	assert.Contains(t, body, "実行中")

	composer := body[strings.Index(body, `id="chat-composer"`):]
	textarea := composer[strings.Index(composer, "<textarea"):strings.Index(composer, "</textarea>")]
	assert.NotContains(t, textarea, "disabled", "input stays possible while generating")
	submit := composer[strings.Index(composer, `type="submit"`):]
	assert.NotContains(t, submit[:strings.Index(submit, ">")], "disabled")

	rec := a.form(http.MethodPost, "/projects/"+id+"/messages", url.Values{"content": {"生成中でも送れる"}})
	assert.Equal(t, http.StatusOK, rec.Code)
}

// announceProgress posts a progress card for a new job in the given status.
func (a *app) announceProgress(projectID, text string, status domain.JobStatus) (jobID, messageID string) {
	a.t.Helper()
	ctx := context.Background()
	jobs := repository.NewJobRepository(a.db)
	job, err := jobs.Create(ctx, domain.JobGenerate, nil)
	require.NoError(a.t, err)
	if status == domain.JobRunning {
		require.NoError(a.t, jobs.MarkRunning(ctx, job.ID))
	}
	require.NoError(a.t, a.svc.Chat.Announce(ctx, projectID, domain.ChatMessage{Kind: domain.KindProgress, RefID: job.ID, Content: text}))
	return job.ID, lastID(a.t, a.get("/projects/"+projectID+"/messages", true).Body.String())
}

func TestSixCardKindsRenderAndCanBeRepliedTo(t *testing.T) {
	a := newApp(t)
	ctx := context.Background()
	id := a.createProject("美容液の縦型15秒CMを作りたい", file{"key.png", pngBytes})
	view, err := a.svc.Projects.Brief(ctx, id)
	require.NoError(t, err)
	assetID := view.Brief.Assets[0]
	score := 0.91

	require.NoError(t, a.svc.Chat.Announce(ctx, id, domain.ChatMessage{Kind: domain.KindKeyframe, RefID: assetID, Content: "Main Visual"}))
	require.NoError(t, a.svc.Chat.Announce(ctx, id, domain.ChatMessage{Kind: domain.KindTake, RefID: "take-2", Content: domain.EncodeTakeCardBody(domain.TakeCardBody{
		Title: "Take 2", Badge: "AI Pick", Score: &score, Summary: "Product 0.95 / Acting 0.88", VideoURL: "/projects/" + id + "/assets/" + assetID})}))
	require.NoError(t, a.svc.Chat.Announce(ctx, id, domain.ChatMessage{Kind: domain.KindTake, RefID: "take-3", Content: domain.EncodeTakeCardBody(domain.TakeCardBody{
		Title: "Take 3", VideoURL: "javascript:alert(1)"})}))
	a.announceProgress(id, "Scene 03 を生成中", domain.JobRunning)
	_, err = a.svc.Chat.RequestApproval(ctx, id, domain.CheckpointScript, "脚本 v1", "sv1")
	require.NoError(t, err)
	require.NoError(t, a.svc.Chat.RaiseEscalation(ctx, id, "リトライ上限に達しました。どうしますか？", []string{"別モデルで再試行", "この Take で確定"}, "shot-1"))

	body := a.get("/projects/"+id, false).Body.String()
	for _, kind := range []domain.MessageKind{domain.KindQuestion, domain.KindApproval, domain.KindKeyframe, domain.KindTake, domain.KindProgress, domain.KindEscalation} {
		assert.Contains(t, body, `data-kind="`+string(kind)+`"`, kind)
	}
	assert.Contains(t, body, `alt="Keyframe"`)
	assert.Contains(t, body, "AI Pick")
	assert.Contains(t, body, "score 0.91")
	assert.Contains(t, body, `data-take-id="take-2"`)
	assert.NotContains(t, body, "javascript:alert", "only same-origin paths become a video source")
	assert.Contains(t, body, "実行中")
	assert.Contains(t, body, ">別モデルで再試行<")
	assert.GreaterOrEqual(t, strings.Count(body, "data-reply-to="), 4, "Keyframe, Take, Approval and Escalation cards can be replied to")
}

func TestProgressCard_UpdatesInPlaceWithoutPolling(t *testing.T) {
	a := newApp(t)
	ctx := context.Background()
	id := a.createProject("動画")
	jobID, cardID := a.announceProgress(id, "Scene 02 を生成中", domain.JobQueued)

	before := a.get("/projects/"+id+"/messages/"+cardID, true).Body.String()
	assert.Contains(t, before, "待機中")
	assert.Contains(t, before, "animate-spin", "an active job shows a spinner")

	jobs := repository.NewJobRepository(a.db)
	require.NoError(t, jobs.MarkRunning(ctx, jobID))
	require.NoError(t, jobs.MarkSucceeded(ctx, jobID))

	after := a.get("/projects/"+id+"/messages/"+cardID, true).Body.String()
	assert.Contains(t, after, "完了")
	assert.NotContains(t, after, "animate-spin")
	assert.Equal(t, http.StatusNotFound, a.get("/projects/"+id+"/messages/missing", true).Code)
}

func TestMessages_AnsweredQuestionCardIsReplacedInPlaceOutOfBand(t *testing.T) {
	a := newApp(t)
	id := a.createProject("動画を作りたい")
	question := lastID(t, a.get("/projects/"+id+"/messages", true).Body.String())

	rec := a.form(http.MethodPost, "/projects/"+id+"/messages", url.Values{"content": {"商品CM"}, "reply_to_message_id": {question}})
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	i := strings.Index(body, `id="msg-`+question+`"`)
	require.GreaterOrEqual(t, i, 0, "the answered card is part of the response")
	card := body[i:]
	assert.Contains(t, card[:strings.Index(card, "</article>")], `hx-swap-oob="outerHTML"`)
	assert.Contains(t, card[:strings.Index(card, "</article>")], "回答済み")
	assert.NotContains(t, card[:strings.Index(card, "</article>")], "data-options", "no buttons are left on an answered card")
}

// The media type of Content-Type is case-insensitive. MaxBody hands a "Multipart/Form-Data" body of
// this route to the handler's own limit, so the handler must read it as multipart (and apply that
// limit) too, not as a plain form that gin parses unbounded and whose attachments are dropped.
