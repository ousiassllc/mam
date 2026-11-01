package router_test

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/testenv"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
)

var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)

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
	assert.Contains(t, body, `data-messages-url="/projects/`+id+`/messages"`, "the client fetches the thread from the URL the server gives")
	assert.Contains(t, body, `data-card-url="/projects/`+id+`/messages/`, "every card says where it is fetched again")
	assert.Contains(t, body, `data-next-url="/projects/`+id+`/messages?after=`, "every message says where what is newer is fetched")
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
	assert.Equal(t, "sandbox", rec.Header().Get("Content-Security-Policy"), "the page policy does not replace the asset's own")
	assert.Equal(t, pngBytes, rec.Body.Bytes())

	assert.Equal(t, http.StatusNotFound, a.get("/projects/"+other+"/assets/"+assetID, false).Code)
	assert.Equal(t, http.StatusNotFound, a.get("/projects/"+id+"/assets/../../etc/passwd", false).Code)
}

func TestWorkspace_InitialDisplayOfALongThreadIsFast(t *testing.T) {
	a := newApp(t)
	ctx := context.Background()
	id := a.createProject("動画")
	for i := 0; i < 400; i++ {
		require.NoError(t, a.svc.Chat.Announce(ctx, id, domain.ChatMessage{Kind: domain.KindText, Content: "発言"}))
	}
	// #905: at most 3 s of CPU whatever the load, and 3 s of clock times the starvation of the machine (1 when idle).
	var rec *httptest.ResponseRecorder
	starved := testenv.Starvation()
	start := time.Now()
	cpu := testenv.CPUTime(func() { rec = a.get("/projects/"+id, false) })
	elapsed := time.Since(start)
	starved = max(starved, testenv.Starvation())
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Greater(t, strings.Count(rec.Body.String(), "data-message-id"), 400)
	assert.Less(t, cpu, 3*time.Second, "non-functional.md §2: initial display within 3 seconds of CPU time")
	assert.Less(t, elapsed, time.Duration(float64(3*time.Second)*starved), "non-functional.md §2: initial display within 3 seconds on the clock, stretched by the starvation of the machine (%.1fx)", starved)
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
