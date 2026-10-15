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

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

const plannerAnswer = `{"shot_objective":"A woman turns and runs","duration":"4.0s","camera":{"framing":"medium"},
	"acting":[{"time":"0.0-4.0","action":"runs"}]}`

const (
	directorProposeAnswer = `{"analysis":{"final_frame":"ドアの前"},"candidates":["ドアを開ける","声が聞こえる","照明が消える"]}`
	directorPlanAnswer    = `{"new_scene":false,"shot":{"objective":"ドアを開ける","duration_seconds":4}}`
)

type plannerLLM struct{}

func (plannerLLM) Complete(_ context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
	text := plannerAnswer
	if agent == llm.AgentDirector { // Continue
		text = directorPlanAnswer
		if strings.Contains(req.System, `"candidates"`) {
			text = directorProposeAnswer
		}
	}
	return llm.Response{Text: text, Provider: llm.ProviderAnthropic, Model: "fake"}, nil
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
	return modeladapter.VideoResult{File: modeladapter.BlobOf([]byte("\x00\x00\x00\x18ftypisom")), MimeType: "video/mp4"}, nil
}

type genApp struct {
	*app
	video   *videoFake
	shotID  string
	sceneID string
	project string
}

var shotIDRe = regexp.MustCompile(`id="shot-([0-9a-f-]{36})"`)

func newGenApp(t *testing.T, withModels bool) *genApp { return newGenAppOpts(t, withModels, genOpts{}) }

// genOpts replaces parts of the generation app: the LLM, the video model and ffmpeg.
type genOpts struct {
	llm   llm.Completer
	video modeladapter.VideoBackend
	media *media.Tools
}

func newGenAppOpts(t *testing.T, withModels bool, o genOpts) *genApp {
	t.Helper()
	testenv.Parallel(t)
	keys := credentialtest.New()
	dir := t.TempDir()
	db := testdb.Open(t)
	video := &videoFake{}
	deps := service.Deps{DB: db, Credentials: keys, AssetsDir: filepath.Join(dir, "assets"), Media: o.media}
	deps.LLM = plannerLLM{}
	if o.llm != nil {
		deps.LLM = o.llm
	}
	jobs := jobqueue.New(repository.NewJobRepository(db), jobqueue.Options{})
	deps.Jobs = jobs
	if withModels {
		var backend modeladapter.VideoBackend = video
		if o.video != nil {
			backend = o.video
		}
		backends := modeladapter.Backends{Video: map[domain.ModelName]modeladapter.VideoBackend{domain.ModelKling3: backend, domain.ModelSeedance2: backend}}
		deps.Models = modeladapter.New(modeladapter.DefaultConfig(), backends)
	}
	svc := servicetest.New(t, deps)
	ctx, cancel := context.WithCancel(context.Background())
	jobs.Start(ctx)
	t.Cleanup(func() { cancel(); jobs.Stop() })
	a := &app{t: t, db: db, engine: router.New(svc, router.Options{}), svc: svc, keys: keys, assets: filepath.Join(dir, "assets")}
	a.cookie, a.token = bootstrap(t, a.engine)
	g := &genApp{app: a, video: video}
	g.project = a.createProject("美容液のCMを作りたい")
	g.sceneID, g.shotID = a.createShot(g.project)
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
	assert.Equal(t, 1, strings.Count(body, "/ws/shots/"+g.shotID), "one WebSocket per card: the player follows the progress, shot.ts waits for it")
	assert.Contains(t, body, `<mam-video-player class="mt-2 block" ws-url="/ws/shots/`+g.shotID+`"`)
	assert.NotContains(t, body, `data-ws-url=`)
	assert.Contains(t, body, `data-shot-progress`)
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

func TestContinue_ProposesACardInTheChatAndConfirmAddsAndGeneratesTheNextShot(t *testing.T) {
	g := newGenApp(t, true)
	assert.Equal(t, http.StatusConflict, g.form(http.MethodPost, "/shots/"+g.shotID+"/continue", nil).Code, "nothing has been completed yet")
	assert.Equal(t, http.StatusNotFound, g.form(http.MethodPost, "/shots/nope/continue", nil).Code)
	assert.Equal(t, http.StatusNotFound, g.form(http.MethodPost, "/shots/nope/continue/confirm", url.Values{"delegate": {"true"}}).Code)
	g.form(http.MethodPost, "/shots/"+g.shotID+"/generate", nil)
	g.waitTakes(3)
	require.Eventually(t, func() bool {
		var state string
		return g.db.Reader().QueryRow(`SELECT state FROM shot WHERE id = ?`, g.shotID).Scan(&state) == nil && state == "PLANNING"
	}, 30*time.Second, 10*time.Millisecond)
	generates := func() (n int) {
		require.NoError(t, g.db.Reader().QueryRow(
			`SELECT COUNT(*) FROM human_intervention WHERE project_id = ? AND kind = 'generate'`, g.project).Scan(&n))
		return n
	}
	require.Equal(t, 1, generates(), "the user's Generate on the first Shot")

	rec := g.form(http.MethodPost, "/shots/"+g.shotID+"/continue", nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "/projects/"+g.project, rec.Header().Get("HX-Redirect"))
	body := rec.Body.String()
	assert.Contains(t, body, "A. ドアを開ける")
	assert.Contains(t, body, "最終フレーム: ドアの前")
	assert.Contains(t, body, domain.DelegateOption)

	assert.Equal(t, http.StatusUnprocessableEntity, g.form(http.MethodPost, "/shots/"+g.shotID+"/continue/confirm", nil).Code, "a choice is required")
	g.video.gate = make(chan struct{}) // the new Shot's job waits here, so the card is drawn while it generates
	rec = g.form(http.MethodPost, "/shots/"+g.shotID+"/continue/confirm", url.Values{"delegate": {"true"}})
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Equal(t, "chat:refresh", rec.Header().Get("HX-Trigger"))
	assert.Contains(t, rec.Body.String(), "Shot 2: ドアを開ける")
	assert.Contains(t, rec.Body.String(), "生成中")
	close(g.video.gate)
	assert.Equal(t, 2, generates(), "confirming the direction is the user asking for the new Shot: a generate intervention")
}

func TestContinue_MissingConfigurationIs503(t *testing.T) {
	off := newGenApp(t, false)
	assert.Equal(t, http.StatusServiceUnavailable, off.form(http.MethodPost, "/shots/"+off.shotID+"/continue", nil).Code)
}

// frameSpyLLM records the images of the Continue Agent's requests.
type frameSpyLLM struct {
	plannerLLM
	mu     sync.Mutex
	images [][]llm.Image
}

func (f *frameSpyLLM) Complete(ctx context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
	if agent == llm.AgentDirector {
		f.mu.Lock()
		f.images = append(f.images, req.Images)
		f.mu.Unlock()
	}
	return f.plannerLLM.Complete(ctx, agent, req)
}

func TestContinue_TheAgentSeesTheFinalFrameOfTheTakeWhenFfmpegIsAvailable(t *testing.T) {
	tools, err := media.Look()
	if err != nil {
		testenv.MissingFFmpeg(t, "ffmpeg / ffprobe are not installed")
	}
	spy := &frameSpyLLM{}
	g := newGenAppOpts(t, true, genOpts{llm: spy, video: newMP4Video(t), media: &tools})
	g.form(http.MethodPost, "/shots/"+g.shotID+"/generate", nil)
	g.waitTakes(3)
	require.Eventually(t, func() bool {
		return strings.Count(g.get("/projects/"+g.project, false).Body.String(), ` fps="10"`) == 3
	}, 30*time.Second, 10*time.Millisecond, "the cards hand the fps ffprobe read to the player")
	require.Eventually(t, func() bool {
		var state string
		return g.db.Reader().QueryRow(`SELECT state FROM shot WHERE id = ?`, g.shotID).Scan(&state) == nil && state == "PLANNING"
	}, 30*time.Second, 10*time.Millisecond)

	require.Equal(t, http.StatusCreated, g.form(http.MethodPost, "/shots/"+g.shotID+"/continue", nil).Code)
	require.Equal(t, http.StatusAccepted, g.form(http.MethodPost, "/shots/"+g.shotID+"/continue/confirm", url.Values{"delegate": {"true"}}).Code)

	spy.mu.Lock()
	defer spy.mu.Unlock()
	require.Len(t, spy.images, 2, "Propose and Plan")
	for _, imgs := range spy.images {
		require.Len(t, imgs, 1)
		assert.Equal(t, "image/jpeg", imgs[0].MimeType)
		assert.True(t, len(imgs[0].Data) > 3 && imgs[0].Data[0] == 0xFF && imgs[0].Data[1] == 0xD8, "a JPEG taken from the video")
		assert.Contains(t, imgs[0].Caption, "最終フレーム")
	}
}
