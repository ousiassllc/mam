package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
)

var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)

type app struct {
	t      *testing.T
	db     *repository.DB
	engine *gin.Engine
	svc    *service.Services
	cookie *http.Cookie
	token  string
	assets string // the assets directory
}

func newApp(t *testing.T) *app {
	t.Helper()
	keyring.MockInit()
	dir := t.TempDir()
	db, err := repository.Open(context.Background(), filepath.Join(dir, "mam.db"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	svc := service.New(service.Deps{DB: db, Credentials: credential.NewKeyring(), AssetsDir: filepath.Join(dir, "assets")})
	a := &app{t: t, db: db, engine: router.New(svc, router.Options{}), svc: svc, assets: filepath.Join(dir, "assets")}
	a.cookie, a.token = bootstrap(t, a.engine)
	return a
}

// request sends an unsafe or safe request the way the browser does: cookie + CSRF header (+ HX-Request).
func (a *app) request(method, path string, body []byte, contentType string, htmx bool) *httptest.ResponseRecorder {
	a.t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.AddCookie(a.cookie)
	req.Header.Set(middleware.CSRFHeaderName, a.token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	return do(a.engine, req)
}

func (a *app) get(path string, htmx bool) *httptest.ResponseRecorder {
	return a.request(http.MethodGet, path, nil, "", htmx)
}

func (a *app) form(method, path string, v url.Values) *httptest.ResponseRecorder {
	return a.request(method, path, []byte(v.Encode()), "application/x-www-form-urlencoded", true)
}

type file struct {
	name string
	data []byte
}

func (a *app) multipart(method, path string, fields map[string]string, files ...file) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		require.NoError(a.t, w.WriteField(k, v))
	}
	for _, f := range files {
		part, err := w.CreateFormFile("attachments", f.name)
		require.NoError(a.t, err)
		_, err = part.Write(f.data)
		require.NoError(a.t, err)
	}
	require.NoError(a.t, w.Close())
	return a.request(method, path, buf.Bytes(), w.FormDataContentType(), true)
}

func (a *app) createProject(content string, files ...file) string {
	a.t.Helper()
	rec := a.multipart(http.MethodPost, "/projects", map[string]string{"content": content}, files...)
	require.Equal(a.t, http.StatusCreated, rec.Code, rec.Body.String())
	loc := rec.Header().Get("HX-Redirect")
	require.True(a.t, strings.HasPrefix(loc, "/projects/"), loc)
	return strings.TrimPrefix(loc, "/projects/")
}

func TestProjects_ListPageWithNewProjectFormAndFragment(t *testing.T) {
	a := newApp(t)

	rec := a.get("/projects", false)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "<html")
	assert.Contains(t, body, `id="new-project-form"`)
	assert.Contains(t, body, `hx-post="/projects"`)
	assert.Contains(t, body, "まだプロジェクトがありません")

	id := a.createProject("美容液の縦型15秒CMを作りたい")

	rec = a.get("/projects", true)
	assert.NotContains(t, rec.Body.String(), "<html", "HTMX gets the list fragment only")
	assert.Contains(t, rec.Body.String(), `href="/projects/`+id+`"`)
	assert.Contains(t, rec.Body.String(), "美容液")

	assert.Contains(t, a.get("/", false).Body.String(), `href="/projects/`+id+`"`, "the landing page is the project list")
}

func TestCreateProject_StartsTheThreadWithBriefQuestionAndAttachments(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液の縦型15秒CMを作りたい。上品な雰囲気で", file{"product.png", pngBytes})

	rec := a.get("/projects/"+id, false)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "<html")
	assert.Contains(t, body, `id="chat-thread"`, "chat_thread")
	assert.Contains(t, body, `id="chat-composer"`, "chat_composer")
	assert.Contains(t, body, `data-ws-url="/ws/projects/`+id+`"`)
	assert.Contains(t, body, "美容液の縦型15秒CMを作りたい")
	assert.Contains(t, body, `data-kind="question"`)
	assert.Contains(t, body, "確認です。動画の目的")
	assert.Contains(t, body, ">Instagram広告<", "option buttons")
	assert.Contains(t, body, ">AIに任せる<", "the delegate button")
	assert.Contains(t, body, `/projects/`+id+`/assets/`, "the attachment is shown")
	assert.Contains(t, body, "下書き（Brief 入力中）")

	frag := a.get("/projects/"+id, true)
	assert.NotContains(t, frag.Body.String(), "<html", "HTMX gets the workspace fragment")
	assert.Contains(t, frag.Body.String(), `id="chat-workspace"`)
}

func TestWorkspace_HistoryIsPersistedPerProject(t *testing.T) {
	a := newApp(t)
	first := a.createProject("最初のプロジェクト")
	second := a.createProject("二番目のプロジェクト")
	rec := a.form(http.MethodPost, "/projects/"+first+"/messages", url.Values{"content": {"追加の発言"}})
	require.Equal(t, http.StatusOK, rec.Code)

	body := a.get("/projects/"+first, false).Body.String()
	assert.Contains(t, body, "最初のプロジェクト")
	assert.Contains(t, body, "追加の発言", "reopening shows the history again")
	assert.NotContains(t, body, "二番目のプロジェクト", "one thread per project")
	assert.NotContains(t, a.get("/projects/"+second, false).Body.String(), "追加の発言")
}

func TestWorkspace_UnknownProjectIs404(t *testing.T) {
	a := newApp(t)

	rec := a.get("/projects/missing", false)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "対象が見つかりません")

	rec = a.get("/projects/missing", true)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "none", rec.Header().Get("HX-Reswap"))
	assert.Contains(t, rec.Body.String(), `hx-swap-oob="beforeend"`, "the error is an out-of-band toast")
}

func TestCreateProject_InvalidInputRerendersTheFormWithTheError(t *testing.T) {
	a := newApp(t)

	rec := a.multipart(http.MethodPost, "/projects", map[string]string{"content": "  "})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), `id="new-project-form"`)
	assert.Contains(t, rec.Body.String(), "発言または添付ファイルを入力してください")

	rec = a.multipart(http.MethodPost, "/projects", map[string]string{"content": "動画を作りたい"}, file{"run.exe", pngBytes})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "対応していないファイル形式です")
	assert.Contains(t, rec.Body.String(), "動画を作りたい", "the user's text is kept")

	rec = a.multipart(http.MethodPost, "/projects", map[string]string{"content": "動画を作りたい"}, file{"fake.png", []byte("MZ not an image")})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "拡張子とファイルの内容が一致しません")

	projects, err := a.svc.Projects.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, projects)
}

func TestCreateProject_RequiresCSRFToken(t *testing.T) {
	a := newApp(t)
	req := httptest.NewRequest(http.MethodPost, "/projects", strings.NewReader("content=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	assert.Equal(t, http.StatusForbidden, do(a.engine, req).Code)
}

func TestMessages_PostAppendsAnswerAndAfterReturnsOnlyNewFragments(t *testing.T) {
	a := newApp(t)
	id := a.createProject("動画を作りたい")
	thread := a.get("/projects/"+id+"/messages", true)
	require.Equal(t, http.StatusOK, thread.Code)
	ids := messageIDs(thread.Body.String())
	require.Len(t, ids, 2)

	rec := a.form(http.MethodPost, "/projects/"+id+"/messages", url.Values{"content": {"商品CMで、Instagram広告向け"}})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "商品CMで、Instagram広告向け")
	assert.Contains(t, rec.Body.String(), `data-kind="question"`, "the AI's next question is in the response")
	assert.Contains(t, rec.Body.String(), `id="project-state"`, "the state badge is refreshed out-of-band")

	newer := a.get("/projects/"+id+"/messages?after="+ids[len(ids)-1], true)
	require.Equal(t, http.StatusOK, newer.Code)
	newIDs := messageIDs(newer.Body.String())
	assert.Len(t, newIDs, 2, "only the two messages of the last turn")
	assert.NotContains(t, newer.Body.String(), "動画を作りたい")
	for _, old := range ids {
		assert.NotContains(t, newIDs, old)
	}
	assert.Empty(t, messageIDs(a.get("/projects/"+id+"/messages?after="+newIDs[len(newIDs)-1], true).Body.String()))

	assert.Equal(t, http.StatusNotFound, a.get("/projects/missing/messages", true).Code)
}

func TestMessages_EmptyMessageIsRejectedWithAToast(t *testing.T) {
	a := newApp(t)
	id := a.createProject("動画")
	rec := a.form(http.MethodPost, "/projects/"+id+"/messages", url.Values{"content": {" "}})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "none", rec.Header().Get("HX-Reswap"))
	assert.Contains(t, rec.Body.String(), "発言または添付ファイルを入力してください")
}

func TestMessages_ReplyToCardIsShownAsQuotedTarget(t *testing.T) {
	a := newApp(t)
	id := a.createProject("動画を作りたい")
	card := lastID(t, a.get("/projects/"+id+"/messages", true).Body.String())

	rec := a.multipart(http.MethodPost, "/projects/"+id+"/messages", map[string]string{"content": "AIに任せる", "reply_to_message_id": card})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "返信: 確認です。")

	rec = a.form(http.MethodPost, "/projects/"+id+"/messages", url.Values{"content": {"x"}, "reply_to_message_id": {"missing"}})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestMessages_AttachmentsAreValidatedOnUpload(t *testing.T) {
	a := newApp(t)
	id := a.createProject("動画")

	rec := a.multipart(http.MethodPost, "/projects/"+id+"/messages", map[string]string{"content": "x"}, file{"a.png", pngBytes})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "data-attachments")

	rec = a.multipart(http.MethodPost, "/projects/"+id+"/messages", map[string]string{"content": "x"}, file{"a.svg", []byte("<svg/>")})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestAssets_ServedWithOwnTypeAndOnlyInsideTheirProject(t *testing.T) {
	a := newApp(t)
	id := a.createProject("動画", file{"a.png", pngBytes})
	other := a.createProject("別の動画")
	view, err := a.svc.Projects.Brief(context.Background(), id)
	require.NoError(t, err)
	assetID := view.Brief.Assets[0]

	rec := a.get("/projects/"+id+"/assets/"+assetID, false)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, pngBytes, rec.Body.Bytes())

	assert.Equal(t, http.StatusNotFound, a.get("/projects/"+other+"/assets/"+assetID, false).Code)
	assert.Equal(t, http.StatusNotFound, a.get("/projects/"+id+"/assets/../../etc/passwd", false).Code)
}

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
	card := lastID(t, a.get("/projects/"+id+"/messages", true).Body.String()) // the newest message is the card

	rec := a.form(http.MethodPost, "/projects/"+id+"/approvals/"+card, url.Values{"decision": {"approve"}, "comment": {"OK"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "承認済み")
	assert.NotContains(t, rec.Body.String(), ">修正を依頼<", "an answered card has no buttons")
	assert.Contains(t, rec.Body.String(), `data-state="VISUAL_DESIGN"`, "the badge shows the new state")
	assert.Equal(t, "chat:refresh", rec.Header().Get("HX-Trigger"), "the thread fetches the follow-up messages")

	again := a.form(http.MethodPost, "/projects/"+id+"/approvals/"+card, url.Values{"decision": {"request_changes"}})
	assert.Equal(t, http.StatusConflict, again.Code)
	assert.Equal(t, "none", again.Header().Get("HX-Reswap"))

	bad := a.form(http.MethodPost, "/projects/"+id+"/approvals/"+card, url.Values{"decision": {"later"}})
	assert.Equal(t, http.StatusUnprocessableEntity, bad.Code)
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

func TestConfirmationLevel_ChangesWhichCheckpointsAsk(t *testing.T) {
	a := newApp(t)
	ctx := context.Background()
	id := a.createProject("動画")

	body := a.get("/projects/"+id, false).Body.String()
	assert.Contains(t, body, `hx-put="/projects/`+id+`/confirmation-level"`)
	assert.Contains(t, body, `value="key_check" selected`)
	assert.NotContains(t, body, `name="checkpoints"`, "the selection appears only for CUSTOM CHECK")

	rec := a.form(http.MethodPut, "/projects/"+id+"/confirmation-level", url.Values{"level": {"custom_check"}, "checkpoints": {"scene_complete"}})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `value="custom_check" selected`)
	assert.Contains(t, rec.Body.String(), `name="checkpoints" value="scene_complete" checked`)
	assert.NotContains(t, rec.Body.String(), `value="script" checked`)

	posted, err := a.svc.Chat.RequestApproval(ctx, id, domain.CheckpointSceneComplete, "Scene 1", "s1")
	require.NoError(t, err)
	assert.True(t, posted)
	posted, err = a.svc.Chat.RequestApproval(ctx, id, domain.CheckpointFinal, "Final", "e1")
	require.NoError(t, err)
	assert.False(t, posted)

	rec = a.form(http.MethodPut, "/projects/"+id+"/confirmation-level", url.Values{"level": {"whenever"}})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "none", rec.Header().Get("HX-Reswap"))
	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPut, "/projects/missing/confirmation-level", url.Values{"level": {"full_auto"}}).Code)
}

func TestComposer_HasModeAndQualityControlsAndStaysEnabledWhileGenerating(t *testing.T) {
	a := newApp(t)
	id := a.createProject("動画")
	a.announceProgress(id, "Scene 02 を生成中", domain.JobRunning)

	body := a.get("/projects/"+id, false).Body.String()
	assert.Contains(t, body, `name="mode"`)
	assert.Contains(t, body, `name="quality_policy"`)
	assert.Contains(t, body, `hx-put="/projects/`+id+`/quality-policy"`, "the Quality control switches the policy")
	assert.Contains(t, body, `value="auto" checked`)
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

func TestWebSocket_SendsNotificationsOnlyNoBodies(t *testing.T) {
	a := newApp(t)
	id := a.createProject("動画を作りたい")
	server := httptest.NewServer(a.engine)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/projects/"+id, nil)
	require.NoError(t, err)
	defer func() { _ = conn.CloseNow() }()

	rec := a.form(http.MethodPost, "/projects/"+id+"/messages", url.Values{"content": {"Instagram広告 の秘密の本文"}})
	require.Equal(t, http.StatusOK, rec.Code)
	newest := lastID(t, a.get("/projects/"+id+"/messages", true).Body.String())

	var raw json.RawMessage
	require.NoError(t, wsjson.Read(ctx, conn, &raw))
	var event map[string]string
	require.NoError(t, json.Unmarshal(raw, &event))
	assert.Equal(t, map[string]string{"type": "message.created", "message_id": newest}, event, "only type and id; no content")
	assert.NotContains(t, string(raw), "秘密の本文")

	jobID, cardID := a.announceProgress(id, "生成中", domain.JobQueued)
	require.NoError(t, wsjson.Read(ctx, conn, &raw)) // message.created for the card
	require.NoError(t, repository.NewJobRepository(a.db).MarkRunning(ctx, jobID))
	a.svc.Chat.JobChanged(ctx, jobID)
	require.NoError(t, wsjson.Read(ctx, conn, &raw))
	require.NoError(t, json.Unmarshal(raw, &event))
	assert.Equal(t, map[string]string{"type": "card.updated", "message_id": cardID}, event)
}

func TestWebSocket_UnknownProjectAndForeignOriginAreRefused(t *testing.T) {
	a := newApp(t)
	id := a.createProject("動画")
	server := httptest.NewServer(a.engine)
	defer server.Close()
	base := "ws" + strings.TrimPrefix(server.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, resp, err := websocket.Dial(ctx, base+"/ws/projects/missing", nil)
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	_, resp, err = websocket.Dial(ctx, base+"/ws/projects/"+id, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"https://evil.example.com"}}})
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestBrief_ConfirmAndEditInTheAuxiliaryView(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液の縦型15秒CMを作りたい。上品な雰囲気で", file{"product.png", pngBytes})

	page := a.get("/projects/"+id+"/brief", false)
	require.Equal(t, http.StatusOK, page.Code)
	body := page.Body.String()
	assert.Contains(t, body, "<html")
	assert.Contains(t, body, `hx-put="/projects/`+id+`/brief"`)
	assert.Contains(t, body, `name="subject" type="text" value="美容液"`)
	assert.Contains(t, body, `name="duration" type="text" value="15秒"`)
	assert.Contains(t, body, `name="aspect_ratio" type="text" value="9:16"`)
	assert.Contains(t, body, "data-attachments", "uploaded assets are listed")
	assert.NotContains(t, a.get("/projects/"+id+"/brief", true).Body.String(), "<html")
	assert.Contains(t, a.get("/projects/"+id, false).Body.String(), `/projects/`+id+`/brief`, "reachable from the ⋯ menu")

	rec := a.form(http.MethodPut, "/projects/"+id+"/brief", url.Values{
		"content_type": {"商品CM"}, "subject": {"化粧水"}, "duration": {"30秒"}, "aspect_ratio": {"16:9"},
		"additional_instruction": {"夜のシーン"}, "references": {"https://example.com/ref"},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "保存しました")
	assert.Contains(t, rec.Body.String(), `value="化粧水"`)

	saved := a.get("/projects/"+id+"/brief", false).Body.String()
	assert.Contains(t, saved, `value="30秒"`)
	assert.Contains(t, saved, "https://example.com/ref")
	assert.Contains(t, a.get("/projects/"+id+"/messages", true).Body.String(), "Brief を更新しました", "the edit is recorded in the thread")
}

func TestBrief_InvalidEditRerendersWithErrorAndKeepsInput(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液のCM")

	rec := a.form(http.MethodPut, "/projects/"+id+"/brief", url.Values{"subject": {"化粧水"}, "aspect_ratio": {"縦長"}})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "9:16 のように")
	assert.Contains(t, rec.Body.String(), `value="縦長"`, "the rejected input is shown again")

	rec = a.form(http.MethodPut, "/projects/"+id+"/brief", url.Values{"references": {"javascript:alert(1)"}})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "javascript:alert(1)")
	assert.NotContains(t, a.get("/projects/"+id+"/brief", false).Body.String(), "javascript:alert", "rejected input is not stored")

	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPut, "/projects/missing/brief", url.Values{}).Code)
	assert.Equal(t, http.StatusNotFound, a.get("/projects/missing/brief", false).Code)
}

func TestWorkspace_InitialDisplayOfALongThreadIsFast(t *testing.T) {
	a := newApp(t)
	ctx := context.Background()
	id := a.createProject("動画")
	for i := 0; i < 400; i++ {
		require.NoError(t, a.svc.Chat.Announce(ctx, id, domain.ChatMessage{Kind: domain.KindText, Content: "発言"}))
	}

	start := time.Now()
	rec := a.get("/projects/"+id, false)
	elapsed := time.Since(start)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Greater(t, strings.Count(rec.Body.String(), "data-message-id"), 400)
	assert.Less(t, elapsed, 3*time.Second, "non-functional.md §2: initial display within 3 seconds")
}

func messageIDs(html string) []string {
	var ids []string
	const marker = `data-message-id="`
	for {
		i := strings.Index(html, marker)
		if i < 0 {
			return ids
		}
		html = html[i+len(marker):]
		ids = append(ids, html[:strings.Index(html, `"`)])
	}
}

func lastID(t *testing.T, html string) string {
	t.Helper()
	ids := messageIDs(html)
	require.NotEmpty(t, ids)
	return ids[len(ids)-1]
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
