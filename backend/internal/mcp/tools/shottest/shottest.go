// Package shottest is the test environment of the Scene / Shot / Take tool packages: an MCP test client
// over services with a fake Reasoning LLM, fake image / video models, a frame source for the Judge and a
// running job queue, plus a Project with one Scene and one Shot. Only tests import it.
package shottest

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/judge"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

// Canned answers of the fake Reasoning LLM.
const (
	SpecAnswer = `{"shot_objective":"A woman turns and runs","duration":"4.0s","camera":{"framing":"medium"},
		"character":{"id":"Mira","identity":"locked"}}`
	ArtAnswer = `{"references":[{"kind":"character","name":"Mio","prompt":"A young woman in a black coat, full body"},
		{"kind":"keyframe","prompt":"The woman on the rainy street, medium shot"}]}`
	// FailVerdict fails a Take on a plain camera issue; the Judge recommends Partial Regeneration.
	FailVerdict = `{"result":"fail","overall_confidence":0.8,"issues":[{"type":"camera_speed","severity":"medium","dimension":"camera_motion"}],
		"preserve":["character"],"repair_strategy":{"action":"partial_regeneration","instructions":["reduce_camera_speed_40_percent"]}}`
	// PassVerdict passes a Take.
	PassVerdict = `{"result":"pass","overall_confidence":0.93,"issues":[],"preserve":["character","acting"]}`
	// Propose / Plan are the Continue Agent's answers.
	ProposeAnswer = `{"analysis":{"final_frame":"ドアの前で振り返る主人公","story":"主人公が部屋に戻った","character":"黒いコート","position":"玄関ドア前",
		"emotion":"不安","object":"","camera":"ミディアムで静止","lighting":"冷たい青","motion":"停止","dialogue":"「誰？」","music":"低いドローン"},
		"candidates":["彼女がドアを開ける","背後から声が聞こえる","突然照明が消える"]}`
	PlanAnswer = `{"new_scene":false,"shot":{"objective":"ドアを開けて廊下を見る","duration_seconds":4}}`
)

// Fixture is an MCP test client and the Project it works on.
type Fixture struct {
	*mcptest.Env
	Jobs                       *jobqueue.Queue
	ProjectID, SceneID, ShotID string
	// Gate, when set before a generation starts, holds the fake video model until it is closed.
	Video *Video
}

// Options adjust the fixture.
type Options struct {
	// Verdict is the Video Judge's answer (default PassVerdict).
	Verdict string
	// Command is the one operation (JSON) the Command Interpreter answers with; empty: no operation.
	Command string
	// FFmpeg turns on ffmpeg (previews, thumbnails, Partial Take Use) and makes the video model write real
	// videos; the test is skipped where ffmpeg is missing.
	FFmpeg bool
	// NoModels leaves out the video / image models (generation is then unavailable).
	NoModels bool
	// Models, when set, is the Model Adapter instead of the fake models (a test that runs a real backend
	// against a fake server, as the local models test does). It is still the Project's Models: Video is unused.
	Models *modeladapter.Adapter
}

// New builds the fixture: register adds the tools under test.
func New(t *testing.T, register func(env *mcptest.Env, r *mcpkit.Registrar), o Options) *Fixture {
	t.Helper()
	video := &Video{}
	var tools *media.Tools
	if o.FFmpeg {
		found, err := media.Look()
		if err != nil {
			testenv.MissingFFmpeg(t, "ffmpeg / ffprobe are not installed")
		}
		tools, video.mp4 = &found, testenv.PatternVideo(t, "320x240", 4)
	}
	f := &Fixture{Video: video}
	l := &llmFake{verdict: o.Verdict, command: o.Command}
	if l.verdict == "" {
		l.verdict = PassVerdict
	}
	f.Env = mcptest.New(t, register, func(d *service.Deps) {
		f.Jobs = jobqueue.New(repository.NewJobRepository(d.DB), jobqueue.Options{})
		d.Jobs, d.LLM, d.Frames, d.Media = f.Jobs, l, frames{}, tools
		if o.Models != nil {
			d.Models = o.Models
		} else if !o.NoModels {
			backends := modeladapter.Backends{Image: map[domain.ModelName]modeladapter.ImageBackend{}, Video: map[domain.ModelName]modeladapter.VideoBackend{}}
			for _, m := range domain.ImageModels() {
				backends.Image[m] = video
			}
			for _, m := range domain.VideoModels() {
				backends.Video[m] = video
			}
			noSleep := func(context.Context, time.Duration) error { return nil }
			d.Models = modeladapter.New(modeladapter.DefaultConfig(), backends, modeladapter.WithSleep(noSleep))
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	f.Jobs.Start(ctx)
	t.Cleanup(func() { cancel(); f.Jobs.Stop() })

	p, err := f.Services.Projects.Create(ctx, service.CreateProjectInput{Content: "美容液のCMを作りたい"})
	require.NoError(t, err)
	f.ProjectID = p.ID
	// DIRECTOR MODE: nothing is judged or repaired by itself unless a test asks.
	_, err = f.Services.Director.SetMode(ctx, p.ID, domain.ModeDirector)
	require.NoError(t, err)
	scene, err := f.Services.Plans.CreateScene(ctx, p.ID, service.SceneForm{Title: "Street"})
	require.NoError(t, err)
	f.SceneID = scene.Scene.ID
	shot, err := f.Services.Plans.CreateShot(ctx, f.SceneID, service.ShotForm{Objective: "主人公が振り返って走り出す", DurationSeconds: "4"})
	require.NoError(t, err)
	f.ShotID = shot.Shots[0].ID
	return f
}

// Generate makes n Takes by calling the service and waits until the Shot is free again and every Take is judged.
func (f *Fixture) Generate(t *testing.T) []string {
	t.Helper()
	_, err := f.Services.Generation.GenerateShot(context.Background(), f.ShotID, "")
	require.NoError(t, err)
	f.WaitIdle(t)
	frag, err := f.Services.Generation.Shot(context.Background(), f.ShotID)
	require.NoError(t, err)
	ids := make([]string, len(frag.Takes))
	for i, tk := range frag.Takes {
		ids[i] = tk.ID
	}
	require.NotEmpty(t, ids, "generation made no Take")
	return ids
}

// WaitIdle waits until the Shot is not being generated, judged or repaired and no job is queued or running.
func (f *Fixture) WaitIdle(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool {
		var state string
		var active int
		if f.DB.Reader().QueryRow(`SELECT state FROM shot WHERE id = ?`, f.ShotID).Scan(&state) != nil {
			return false
		}
		_ = f.DB.Reader().QueryRow(`SELECT COUNT(*) FROM job WHERE status IN ('queued', 'running')`).Scan(&active)
		return !domain.ShotState(state).Busy() && active == 0
	}, 30*time.Second, 10*time.Millisecond, "the Shot never became idle")
}

// Exec runs a statement on the test database.
func (f *Fixture) Exec(t *testing.T, q string, args ...any) {
	t.Helper()
	require.NoError(t, f.DB.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, q, args...)
		return err
	}))
}

// Count runs a COUNT query.
func (f *Fixture) Count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, f.DB.Reader().QueryRow(q, args...).Scan(&n))
	return n
}

// Video is the image and video backend of every model; it writes no real video.
type Video struct {
	mu   sync.Mutex
	gate chan struct{}
	err  error
	mp4  []byte // the video to write; nil: a stub
}

// FailWith makes every generation fail with err.
func (v *Video) FailWith(err error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.err = err
}

// Hold makes every generation wait until the returned function is called.
func (v *Video) Hold() (release func()) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.gate = make(chan struct{})
	gate := v.gate
	var once sync.Once
	return func() { once.Do(func() { close(gate) }) }
}

func (v *Video) wait(ctx context.Context) error {
	v.mu.Lock()
	gate, err := v.gate, v.err
	v.mu.Unlock()
	if err != nil {
		return err
	}
	if gate == nil {
		return nil
	}
	select {
	case <-gate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// GenerateVideo implements modeladapter.VideoBackend.
func (v *Video) GenerateVideo(ctx context.Context, _ string, _ modeladapter.VideoRequest) (modeladapter.VideoResult, error) {
	if err := v.wait(ctx); err != nil {
		return modeladapter.VideoResult{}, err
	}
	data := v.mp4
	if data == nil {
		data = []byte("\x00\x00\x00\x18ftypisom")
	}
	return modeladapter.VideoResult{File: modeladapter.BlobOf(data), MimeType: "video/mp4"}, nil
}

// GenerateImage implements modeladapter.ImageBackend.
func (v *Video) GenerateImage(ctx context.Context, _ string, _ modeladapter.ImageRequest) (modeladapter.ImageResult, error) {
	if err := v.wait(ctx); err != nil {
		return modeladapter.ImageResult{}, err
	}
	return modeladapter.ImageResult{Data: append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...), MimeType: "image/png"}, nil
}

// llmFake answers every Agent with canned JSON.
type llmFake struct{ verdict, command string }

func (l *llmFake) Complete(_ context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
	text := SpecAnswer
	switch agent {
	case llm.AgentArtDirector:
		text = ArtAnswer
	case llm.AgentVideoJudge:
		text = l.verdict
	case llm.AgentCommandInterpreter:
		text = `{"operations":[]}`
		if l.command != "" {
			text = `{"operations":[` + l.command + `]}`
		}
	case llm.AgentDirector:
		text = PlanAnswer
		if strings.Contains(req.System, `"candidates"`) {
			text = ProposeAnswer
		}
	}
	return llm.Response{Text: text, Provider: llm.ProviderAnthropic, Model: "fake"}, nil
}

// frames shows the Judge one picture for every Take.
type frames struct{}

func (frames) Sample(_ context.Context, _ string, _ judge.SampleSpec) (judge.Clip, error) {
	clip := judge.Clip{Duration: 4, FPS: 25, Interval: 4}
	n, _ := clip.FrameRange(0, 0)
	clip.Frames = []judge.Frame{{Time: 0, Number: n, JPEG: []byte("jpeg")}}
	return clip, nil
}
