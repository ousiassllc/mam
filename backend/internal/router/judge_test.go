package router_test

import (
	"bytes"
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/judge"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/repair"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/facesim"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

// The Shot Spec names the Character the way the Prompt Planner does: "identity": "locked".
const lockedSpecAnswer = `{"shot_objective":"A woman turns and runs","duration":"4.0s","camera":{"framing":"medium"},
	"character":{"id":"Mira","identity":"locked"}}`

// judgeLLM answers the Prompt Planner with a spec and the Video Judge with the given verdict.
type judgeLLM struct {
	mu      sync.Mutex
	verdict string
	specs   string
	repair  string // the Repair Agent's answer; empty: the Shot Spec (which is no valid choice)
	command string // the Command Interpreter's answer; empty: the Shot Spec (which holds no operation)
	judged  int
}

func (l *judgeLLM) Complete(_ context.Context, agent llm.Agent, _ llm.Request) (llm.Response, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	text := l.specs
	switch {
	case agent == llm.AgentVideoJudge:
		l.judged++
		text = l.verdict
	case agent == llm.AgentRepair && l.repair != "":
		text = l.repair
	case agent == llm.AgentCommandInterpreter && l.command != "":
		text = l.command
	}
	return llm.Response{Text: text, Provider: llm.ProviderGoogle, Model: "fake"}, nil
}

func (l *judgeLLM) judgeCalls() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.judged
}

// A picture's red channel is the face in it: red r is the unit embedding at angle r/255·π/2, so
// the similarity to red 0 (Mira's reference face) is cos(angle).
func faceJPEG(t *testing.T, sim float64) []byte {
	t.Helper()
	red := uint8(math.Round(math.Acos(sim) / (math.Pi / 2) * 255))
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			img.Set(x, y, color.RGBA{R: red, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, &jpeg.Options{Quality: 100}))
	return buf.Bytes()
}

type redFaces struct{}

func (redFaces) Faces(_ context.Context, img image.Image) ([]facesim.Face, error) {
	r, _, _, _ := img.At(0, 0).RGBA()
	a := float64(r>>8) / 255 * math.Pi / 2
	return []facesim.Face{{Embedding: facesim.Embedding{float32(math.Cos(a)), float32(math.Sin(a))}}}, nil
}

// clipFrames is a FrameSource that shows the same frames for every Take.
type clipFrames struct{ pictures [][]byte }

func (c clipFrames) Sample(_ context.Context, _ string, _ judge.SampleSpec) (judge.Clip, error) {
	clip := judge.Clip{Duration: 4, FPS: 25, Interval: 4 / float64(len(c.pictures))}
	for i, p := range c.pictures {
		tm := float64(i) * clip.Interval
		n, _ := clip.FrameRange(tm, tm)
		clip.Frames = append(clip.Frames, judge.Frame{Time: tm, Number: n, JPEG: p})
	}
	return clip, nil
}

type judgeApp struct {
	*genApp
	llm *judgeLLM
	dir string
}

// newJudgeApp is a generation app with the Judge on: Mira has a face reference asset (red 0),
// and every Take shows the given similarities.
func newJudgeApp(t *testing.T, verdict string, sims ...float64) *judgeApp {
	t.Helper()
	return newJudgeAppWith(t, verdict, nil, sims...)
}

// newJudgeAppWith is newJudgeApp with a Jev client for the Repair Agent's shadow run (nil: none).
func newJudgeAppWith(t *testing.T, verdict string, shadow repair.Shadow, sims ...float64) *judgeApp {
	t.Helper()
	return newJudgeAppOpts(t, judgeOpts{verdict: verdict, shadow: shadow}, sims...)
}

// judgeOpts are the parts of a judgeApp a test replaces.
type judgeOpts struct {
	verdict string
	shadow  repair.Shadow
	// video replaces the fake video backend; media turns on ffmpeg (splicing Partial Regenerations).
	video modeladapter.VideoBackend
	media *media.Tools
	// command is the Command Interpreter's answer (empty: the Shot Spec, which holds no operation).
	command string
	// spec replaces the Prompt Planner's answer (empty: lockedSpecAnswer).
	spec string
	keys *credentialtest.Memory // nil: an empty one (a Jev client in shadow needs the store first)
}

func newJudgeAppOpts(t *testing.T, o judgeOpts, sims ...float64) *judgeApp {
	t.Helper()
	testenv.Parallel(t)
	keys := cmp.Or(o.keys, credentialtest.New())
	dir := t.TempDir()
	db := testdb.Open(t)
	l := &judgeLLM{verdict: o.verdict, specs: cmp.Or(o.spec, lockedSpecAnswer), command: o.command}
	video := &videoFake{}
	jobs := jobqueue.New(repository.NewJobRepository(db), jobqueue.Options{})
	var pics [][]byte
	for _, s := range sims {
		pics = append(pics, faceJPEG(t, s))
	}
	var backend modeladapter.VideoBackend = video
	if o.video != nil {
		backend = o.video
	}
	backends := modeladapter.Backends{Video: map[domain.ModelName]modeladapter.VideoBackend{domain.ModelKling3: backend, domain.ModelSeedance2: backend, domain.ModelRunwayGen45: backend}}
	svc := servicetest.New(t, service.Deps{
		DB: db, Credentials: keys, AssetsDir: filepath.Join(dir, "assets"), LLM: l, Jobs: jobs,
		Models: modeladapter.New(modeladapter.DefaultConfig(), backends), Frames: clipFrames{pics}, Faces: redFaces{}, Jev: o.shadow, Media: o.media,
	})
	ctx, cancel := context.WithCancel(context.Background())
	jobs.Start(ctx)
	t.Cleanup(func() { cancel(); jobs.Stop() })
	a := &app{t: t, db: db, engine: router.New(svc, router.Options{}), svc: svc, keys: keys}
	a.cookie, a.token = bootstrap(t, a.engine)
	g := &genApp{app: a, video: video}
	g.project = a.createProject("美容液のCMを作りたい")
	g.sceneID, g.shotID = a.createShot(g.project)

	// Mira and her face reference: a stored picture (red 0) and its asset row.
	rel := filepath.Join(g.project, "mira.jpg")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "assets", g.project), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "assets", rel), faceJPEG(t, 1.0), 0o600))
	require.NoError(t, db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO asset (id, project_id, type, file_path, mime_type, size_bytes) VALUES ('face-1', ?, 'image', ?, 'image/jpeg', 1)`, g.project, rel); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO character (id, project_id, name, face_reference_asset_id) VALUES ('char-1', ?, 'Mira', 'face-1')`, g.project)
		return err
	}))
	return &judgeApp{genApp: g, llm: l, dir: dir}
}

// results counts the Judge Results of the Take; c fails instead of the test (see countOn).
func (j *judgeApp) results(c require.TestingT, takeID string) int {
	return j.countOn(c, `SELECT COUNT(*) FROM judge_result WHERE take_id = ?`, takeID)
}

// generateAndJudge generates the Shot's Takes and waits until each of them has been judged.
func (j *judgeApp) generateAndJudge() []string {
	j.t.Helper()
	ids := j.generateTakes()
	require.EventuallyWithT(j.t, func(c *assert.CollectT) {
		for _, id := range ids {
			assert.Positive(c, j.results(c, id))
		}
	}, 30*time.Second, 10*time.Millisecond, "every Take is judged as soon as it is made")
	require.Eventually(j.t, func() bool {
		var state string
		return j.db.Reader().QueryRow(`SELECT state FROM shot WHERE id = ?`, j.shotID).Scan(&state) == nil && state == "PLANNING"
	}, 30*time.Second, 10*time.Millisecond, "the Shot is free again")
	// The Shot is released inside the job handler, a moment before the queue marks the job done;
	// a re-run posted in between would get 409.
	require.Eventually(j.t, func() bool {
		var active int
		return j.db.Reader().QueryRow(`SELECT COUNT(*) FROM job WHERE kind = 'judge' AND status IN ('queued', 'running')`).Scan(&active) == nil && active == 0
	}, 30*time.Second, 10*time.Millisecond, "every judge job is finished")
	return ids
}

type judgeBody struct {
	Result            string  `json:"result"`
	OverallConfidence float64 `json:"overall_confidence"`
	Metrics           struct {
		Status       string `json:"face_identity_status"`
		FaceIdentity []struct {
			CharacterID     string  `json:"character_id"`
			SimilarityMean  float64 `json:"similarity_mean"`
			SimilarityMin   float64 `json:"similarity_min"`
			SampledFrames   int     `json:"sampled_frames"`
			UndetectedRatio float64 `json:"undetected_ratio"`
		} `json:"face_identity"`
	} `json:"metrics"`
	Issues []struct {
		Type          string `json:"type"`
		Severity      string `json:"severity"`
		TimeRange     string `json:"time_range"`
		Frames        []int  `json:"frames"`
		LockViolation bool   `json:"lock_violation"`
		BrandID       string `json:"brand_constraint_id"`
	} `json:"issues"`
	Preserve       []string `json:"preserve"`
	RepairStrategy *struct {
		Action       string   `json:"action"`
		Instructions []string `json:"instructions"`
	} `json:"repair_strategy"`
}

func (j *judgeApp) result(takeID string) (int, judgeBody) {
	j.t.Helper()
	rec := j.get("/api/v1/takes/"+takeID+"/judge-result", false)
	var b judgeBody
	if rec.Code == http.StatusOK {
		require.NoError(j.t, json.Unmarshal(rec.Body.Bytes(), &b), rec.Body.String())
	}
	return rec.Code, b
}

const goodVerdict = `{"result":"pass","overall_confidence":0.93,"issues":[],"preserve":["character","acting"]}`

func TestJudge_EveryGeneratedTakeIsJudgedAndTheAPIReturnsTheResult(t *testing.T) {
	// A steady face all along.
	j := newJudgeApp(t, goodVerdict, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95)
	ids := j.generateAndJudge()

	for _, id := range ids {
		code, body := j.result(id)
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, "pass", body.Result)
		assert.InDelta(t, 0.93, body.OverallConfidence, 1e-9)
		assert.Equal(t, []string{"character", "acting"}, body.Preserve)
		assert.Equal(t, "measured", body.Metrics.Status)
		require.Len(t, body.Metrics.FaceIdentity, 1)
		m := body.Metrics.FaceIdentity[0]
		assert.Equal(t, "Mira", m.CharacterID)
		assert.Equal(t, 8, m.SampledFrames)
		assert.InDelta(t, 0.95, m.SimilarityMean, 0.02)
		assert.Zero(t, m.UndetectedRatio)
		assert.Empty(t, body.Issues)
		assert.Nil(t, body.RepairStrategy)
	}
	assert.Equal(t, 3, j.llm.judgeCalls())

	// The Shot is released when the last Judge job finishes, a moment after its result is stored.
	require.Eventually(t, func() bool {
		page := j.get("/projects/"+j.project+"/scenes/"+j.sceneID+"?shot="+j.shotID, true).Body.String()
		return strings.Contains(page, `hx-post="/shots/`+j.shotID+`/generate`)
	}, 10*time.Second, 10*time.Millisecond, "the Shot is free again once every Take is judged")
}

// addMatchingCanonFace gives Mira a second picture of the same face on the Series canon, so the
// warn / FAIL thresholds are calibrated from that pair (spec §12.4). One reference is not enough.
func (j *judgeApp) addMatchingCanonFace(t *testing.T) {
	t.Helper()
	rel := filepath.Join(j.project, "mira-canon.jpg")
	require.NoError(t, os.WriteFile(filepath.Join(j.dir, "assets", rel), faceJPEG(t, 1.0), 0o600))
	require.NoError(t, j.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO asset (id, project_id, type, file_path, mime_type, size_bytes) VALUES ('face-2', ?, 'image', ?, 'image/jpeg', 1)`, j.project, rel); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO series (id, name) VALUES ('series-1', 'Mira')`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO character (id, series_id, name, face_reference_asset_id) VALUES ('canon-1', 'series-1', 'Mira', 'face-2')`); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE character SET origin_id = 'canon-1' WHERE id = 'char-1'`)
		return err
	}))
}
