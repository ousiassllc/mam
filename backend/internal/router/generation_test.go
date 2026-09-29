package router_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/service"
)

const plannerAnswer = `{"shot_objective":"A woman turns and runs","duration":"4.0s","camera":{"framing":"medium"},
	"acting":[{"time":"0.0-4.0","action":"runs"}]}`

type plannerLLM struct{}

func (plannerLLM) Complete(context.Context, llm.Agent, llm.Request) (llm.Response, error) {
	return llm.Response{Text: plannerAnswer, Provider: llm.ProviderAnthropic, Model: "fake"}, nil
}

// videoFake makes every video; while gate is open it waits for it to be closed.
type videoFake struct {
	mu   sync.Mutex
	gate chan struct{}
}

func (v *videoFake) GenerateVideo(ctx context.Context, _ string, _ modeladapter.VideoRequest) (modeladapter.VideoResult, error) {
	v.mu.Lock()
	gate := v.gate
	v.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return modeladapter.VideoResult{}, ctx.Err()
		}
	}
	return modeladapter.VideoResult{Data: []byte("\x00\x00\x00\x18ftypisom"), MimeType: "video/mp4"}, nil
}

type genApp struct {
	*app
	video   *videoFake
	shotID  string
	sceneID string
	project string
}

var shotIDRe = regexp.MustCompile(`id="shot-([0-9a-f-]{36})"`)

func newGenApp(t *testing.T, withModels bool) *genApp {
	t.Helper()
	keyring.MockInit()
	dir := t.TempDir()
	db, err := repository.Open(context.Background(), filepath.Join(dir, "mam.db"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	video := &videoFake{}
	deps := service.Deps{DB: db, Credentials: credential.NewKeyring(), AssetsDir: filepath.Join(dir, "assets"), LLM: plannerLLM{}}
	jobs := jobqueue.New(repository.NewJobRepository(db), jobqueue.Options{})
	deps.Jobs = jobs
	if withModels {
		backends := modeladapter.Backends{Video: map[domain.ModelName]modeladapter.VideoBackend{domain.ModelKling3: video, domain.ModelSeedance2: video}}
		deps.Models = modeladapter.New(modeladapter.DefaultConfig(), backends)
	}
	svc := service.New(deps)
	ctx, cancel := context.WithCancel(context.Background())
	jobs.Start(ctx)
	t.Cleanup(func() { cancel(); jobs.Stop() })

	a := &app{t: t, db: db, engine: router.New(svc, router.Options{}), svc: svc}
	a.cookie, a.token = bootstrap(t, a.engine)
	g := &genApp{app: a, video: video}
	g.project = a.createProject("美容液のCMを作りたい")
	rec := a.form(http.MethodPost, "/projects/"+g.project+"/scenes", url.Values{"title": {"Street"}})
	m := sceneIDRe.FindStringSubmatch(rec.Body.String())
	require.NotNil(t, m)
	g.sceneID = m[1]
	rec = a.form(http.MethodPost, "/scenes/"+g.sceneID+"/shots", url.Values{"objective": {"走り出す"}, "duration_seconds": {"4"}})
	require.Equal(t, http.StatusCreated, rec.Code)
	m = shotIDRe.FindStringSubmatch(rec.Body.String())
	require.NotNil(t, m, rec.Body.String())
	g.shotID = m[1]
	return g
}

func (g *genApp) waitTakes(n int) {
	g.t.Helper()
	require.Eventually(g.t, func() bool {
		var c int
		return g.db.Reader().QueryRow(`SELECT COUNT(*) FROM take`).Scan(&c) == nil && c == n
	}, 30*time.Second, 10*time.Millisecond)
}

func TestGenerate_Answers202WithTheGeneratingShotCardAndStartsAJob(t *testing.T) {
	g := newGenApp(t, true)
	g.video.gate = make(chan struct{})

	rec := g.form(http.MethodPost, "/shots/"+g.shotID+"/generate", url.Values{"additional_instruction": {"もう少しカメラを寄せて"}})
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.NotContains(t, body, "<html", "a fragment")
	assert.Contains(t, body, `id="shot-`+g.shotID+`"`)
	assert.Contains(t, body, "生成中")
	assert.Contains(t, body, `data-ws-url="/ws/shots/`+g.shotID+`"`, "the card follows its progress over the WebSocket")
	assert.NotContains(t, body, `hx-post="/shots/`+g.shotID+`/generate"`, "no second run can be started from the card")

	rec = g.form(http.MethodPost, "/shots/"+g.shotID+"/generate", nil)
	assert.Equal(t, http.StatusConflict, rec.Code, "a Shot that is being generated cannot be started again")
	assert.Contains(t, rec.Body.String(), "生成中")
	assert.Equal(t, http.StatusConflict, g.form(http.MethodPost, "/shots/"+g.shotID+"/reference-pack", nil).Code)

	close(g.video.gate)
	g.waitTakes(3)
	page := g.get("/projects/"+g.project+"/scenes/"+g.sceneID+"?shot="+g.shotID, true).Body.String()
	require.Eventually(t, func() bool {
		page = g.get("/projects/"+g.project+"/scenes/"+g.sceneID+"?shot="+g.shotID, true).Body.String()
		return strings.Contains(page, `hx-post="/shots/`+g.shotID+`/generate"`)
	}, 30*time.Second, 10*time.Millisecond, "when generation is over the card offers to generate again")
	assert.Contains(t, page, "seedance-2.0", "the Takes are listed with their canonical model name")
	assert.Contains(t, page, `hx-post="/shots/`+g.shotID+`/reference-pack"`)
}

func TestGenerate_UnknownShotIs404AndMissingConfigurationIs503(t *testing.T) {
	g := newGenApp(t, true)
	assert.Equal(t, http.StatusNotFound, g.form(http.MethodPost, "/shots/nope/generate", nil).Code)
	assert.Equal(t, http.StatusNotFound, g.form(http.MethodPost, "/shots/nope/reference-pack", nil).Code)

	off := newGenApp(t, false)
	assert.Equal(t, http.StatusServiceUnavailable, off.form(http.MethodPost, "/shots/"+off.shotID+"/generate", nil).Code)
}

func TestGenerate_BudgetCeilingAnswers402(t *testing.T) {
	g := newGenApp(t, true)
	require.NoError(t, g.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE project SET shot_budget_ceiling = 0.01 WHERE id = ?`, g.project)
		return err
	}))
	rec := g.form(http.MethodPost, "/shots/"+g.shotID+"/generate", nil)
	assert.Equal(t, http.StatusPaymentRequired, rec.Code)
	assert.Contains(t, rec.Body.String(), "予算上限")
	assert.Equal(t, "none", rec.Header().Get("HX-Reswap"), "the shot card stays as it is")
}

func TestShotWebSocket_ReportsIdleThenProgressUntilDone(t *testing.T) {
	g := newGenApp(t, true)
	server := httptest.NewServer(g.engine)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	base := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/shots/"

	idle, _, err := websocket.Dial(ctx, base+g.shotID, nil)
	require.NoError(t, err)
	defer func() { _ = idle.CloseNow() }()
	var p generation.Progress
	require.NoError(t, wsjson.Read(ctx, idle, &p))
	assert.Equal(t, generation.Progress{ShotID: g.shotID, Stage: generation.StageIdle}, p)

	g.video.gate = make(chan struct{})
	require.Equal(t, http.StatusAccepted, g.form(http.MethodPost, "/shots/"+g.shotID+"/generate", nil).Code)
	conn, _, err := websocket.Dial(ctx, base+g.shotID, nil)
	require.NoError(t, err)
	defer func() { _ = conn.CloseNow() }()
	close(g.video.gate)

	var stages []generation.Stage
	for p.Stage != generation.StageDone {
		require.NoError(t, wsjson.Read(ctx, conn, &p), "stages so far: %v", stages)
		stages = append(stages, p.Stage)
		assert.Equal(t, g.shotID, p.ShotID)
	}
	assert.Contains(t, stages, generation.StageGenerating)
}

func TestShotWebSocket_UnknownShotAndForeignOriginAreRefused(t *testing.T) {
	g := newGenApp(t, true)
	server := httptest.NewServer(g.engine)
	defer server.Close()
	base := "ws" + strings.TrimPrefix(server.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, resp, err := websocket.Dial(ctx, base+"/ws/shots/missing", nil)
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	_, resp, err = websocket.Dial(ctx, base+"/ws/shots/"+g.shotID, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"https://evil.example.com"}}})
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}
