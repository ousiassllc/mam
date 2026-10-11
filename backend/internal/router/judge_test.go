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
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/judge"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/repair"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/facesim"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
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
}

func newJudgeAppOpts(t *testing.T, o judgeOpts, sims ...float64) *judgeApp {
	t.Helper()
	keyring.MockInit()
	dir := t.TempDir()
	db, err := repository.Open(context.Background(), filepath.Join(dir, "mam.db"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

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
		DB: db, Credentials: credential.NewKeyring(), AssetsDir: filepath.Join(dir, "assets"), LLM: l, Jobs: jobs,
		Models: modeladapter.New(modeladapter.DefaultConfig(), backends), Frames: clipFrames{pics}, Faces: redFaces{}, Jev: o.shadow, Media: o.media,
	})
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
	require.NotNil(t, m)
	g.shotID = m[1]

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

func (j *judgeApp) takeIDs() []string {
	j.t.Helper()
	rows, err := j.db.Reader().Query(`SELECT id FROM take ORDER BY created_at, id`)
	require.NoError(j.t, err)
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		require.NoError(j.t, rows.Scan(&id))
		ids = append(ids, id)
	}
	return ids
}

func (j *judgeApp) results(takeID string) int {
	var n int
	require.NoError(j.t, j.db.Reader().QueryRow(`SELECT COUNT(*) FROM judge_result WHERE take_id = ?`, takeID).Scan(&n))
	return n
}

// generateAndJudge generates the Shot's Takes and waits until each of them has been judged.
func (j *judgeApp) generateAndJudge() []string {
	j.t.Helper()
	require.Equal(j.t, http.StatusAccepted, j.form(http.MethodPost, "/shots/"+j.shotID+"/generate", nil).Code)
	j.waitTakes(3)
	ids := j.takeIDs()
	require.Eventually(j.t, func() bool {
		for _, id := range ids {
			if j.results(id) == 0 {
				return false
			}
		}
		return true
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

func TestJudge_ALockedIdentityDriftFailsTheTakeWhateverTheModelSays(t *testing.T) {
	// The face drifts for frames 3-5 of 8 (1.5-3.0 s). The model gives a confident pass.
	// Two pictures of Mira let the thresholds come from their similarity (spec §12.4).
	j := newJudgeApp(t, goodVerdict, 0.95, 0.95, 0.95, 0.2, 0.2, 0.2, 0.95, 0.95)
	j.addMatchingCanonFace(t)
	ids := j.generateAndJudge()

	code, body := j.result(ids[0])
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "fail", body.Result)
	assert.InDelta(t, 0.93, body.OverallConfidence, 1e-9, "the model's confidence is reported as it is")
	require.Len(t, body.Issues, 1)
	is := body.Issues[0]
	assert.Equal(t, "face_identity_drift", is.Type)
	assert.Equal(t, "high", is.Severity)
	assert.Equal(t, "1.5-3.0s", is.TimeRange)
	assert.Equal(t, []int{38, 75}, is.Frames)
	assert.True(t, is.LockViolation, "the spec locks Mira's identity")
	require.NotNil(t, body.RepairStrategy)
	assert.Equal(t, "reference_reinforcement", body.RepairStrategy.Action)
	assert.Contains(t, body.RepairStrategy.Instructions, "increase_face_reference_strength")

	rows := j.get("/projects/"+j.project, false).Body.String()
	assert.Contains(t, rows, "LOCK 違反", "the chat says the Take failed on a LOCK violation")
}

func TestJudge_ALockSetOnTheShotMakesAMediumIssueInThatDimensionAViolation(t *testing.T) {
	// The model passes the Take but notes a medium Acting issue. Without a LOCK that is a pass with
	// a remark; with Acting LOCKed on the Shot (PUT /shots/:id/locks) it is a LOCK violation.
	const actingIssue = `{"result":"pass","overall_confidence":0.9,"issues":[{"type":"acting_drift","severity":"medium","dimension":"acting","time_range":"0.8-1.7s"}]}`
	judged := func(lock bool) (judgeBody, *judgeApp) {
		j := newJudgeApp(t, actingIssue, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95)
		if lock {
			require.NoError(t, repository.NewLockRepository(j.db).SetShotLock(context.Background(), j.shotID, domain.ParamActing, domain.LockLocked, nil))
		}
		ids := j.generateAndJudge()
		code, body := j.result(ids[0])
		require.Equal(t, http.StatusOK, code)
		return body, j
	}

	body, _ := judged(false)
	assert.Equal(t, "pass", body.Result, "without the LOCK the model's pass stands")
	require.Len(t, body.Issues, 1)
	assert.False(t, body.Issues[0].LockViolation)

	body, j := judged(true)
	assert.Equal(t, "fail", body.Result, "a LOCK on the Shot is a LOCK for the Judge")
	require.Len(t, body.Issues, 1)
	assert.True(t, body.Issues[0].LockViolation)
	assert.Contains(t, j.get("/projects/"+j.project, false).Body.String(), "LOCK 違反")
}

func TestJudge_PostRerunsTheJudgeAndKeepsEveryResultAsHistory(t *testing.T) {
	j := newJudgeApp(t, goodVerdict, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95)
	ids := j.generateAndJudge()
	take := ids[0]
	require.Equal(t, 1, j.results(take))

	// The next run is a fail: the latest result must be the new one.
	j.llm.mu.Lock()
	j.llm.verdict = `{"result":"fail","overall_confidence":0.8,"issues":[{"type":"camera_speed","severity":"medium","dimension":"camera_motion","time_range":"0.8-1.7s"}],"repair_strategy":{"action":"partial_regeneration","instructions":["reduce_camera_speed_40_percent"]}}`
	j.llm.mu.Unlock()

	rec := j.form(http.MethodPost, "/takes/"+take+"/judge", nil)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "<html", "a fragment")
	assert.Contains(t, rec.Body.String(), `id="shot-`+j.shotID+`"`)
	require.Eventually(t, func() bool { return j.results(take) == 2 }, 30*time.Second, 10*time.Millisecond)

	code, body := j.result(take)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "fail", body.Result, "the latest run")
	require.Len(t, body.Issues, 1)
	assert.Equal(t, "camera_speed", body.Issues[0].Type)
	assert.Equal(t, "0.8-1.7s", body.Issues[0].TimeRange)
	assert.Equal(t, []int{20, 43}, body.Issues[0].Frames)
	require.NotNil(t, body.RepairStrategy)
	assert.Equal(t, "partial_regeneration", body.RepairStrategy.Action)

	var passes int
	require.NoError(t, j.db.Reader().QueryRow(`SELECT COUNT(*) FROM judge_result WHERE take_id = ? AND result = 'pass'`, take).Scan(&passes))
	assert.Equal(t, 1, passes, "the first result is still there")
}

func TestJudge_UnknownTakeIs404AndAnUnjudgedTakeHasNoResult(t *testing.T) {
	j := newJudgeApp(t, goodVerdict, 0.95, 0.95, 0.95)
	assert.Equal(t, http.StatusNotFound, j.form(http.MethodPost, "/takes/nope/judge", nil).Code)
	code, _ := j.result("nope")
	assert.Equal(t, http.StatusNotFound, code)

	ids := j.generateAndJudge()
	require.NoError(t, j.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM judge_result WHERE take_id = ?`, ids[0])
		return err
	}))
	code, _ = j.result(ids[0])
	assert.Equal(t, http.StatusNotFound, code, "the Take exists but was never judged")
}

func TestJudge_AJudgeRunThatIsQueuedOrRunningCannotBeStartedTwice(t *testing.T) {
	j := newJudgeApp(t, goodVerdict, 0.95, 0.95, 0.95)
	ids := j.generateAndJudge()
	// A queued judge job of the Take is on record.
	require.NoError(t, j.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO job (id, kind, payload, status) VALUES ('stuck', 'judge', ?, 'queued')`, `{"take_id":"`+ids[0]+`","shot_id":"`+j.shotID+`"}`)
		return err
	}))
	rec := j.form(http.MethodPost, "/takes/"+ids[0]+"/judge", nil)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.True(t, strings.Contains(rec.Body.String(), "Judge 中"), rec.Body.String())
}

// The Shot card draws a Judge button only for a Take the server accepts a Judge run of (#438): one that
// was never judged (or whose run failed) and is not being judged, on a Shot that is not busy.
func TestJudge_ButtonIsDrawnOnlyForUnjudgedTakesTheServerAccepts(t *testing.T) {
	j := newJudgeApp(t, goodVerdict, 0.95, 0.95, 0.95, 0.95)
	j.director()
	ids := j.generateAndJudge()
	page := func() string {
		return j.get("/projects/"+j.project+"/scenes/"+j.sceneID+"?shot="+j.shotID, true).Body.String()
	}
	button := func(html, take string) bool { return strings.Contains(html, "/takes/"+take+"/judge\"") }

	assert.False(t, button(page(), ids[0]), "judged: no button")
	assert.False(t, button(page(), ids[1]), "judged: no button")

	j.execSQL(`DELETE FROM judge_result WHERE take_id = ?`, ids[1])
	html := page()
	assert.True(t, button(html, ids[1]), "never judged")
	assert.False(t, button(html, ids[0]))

	// A judge run of the Take in flight: the server answers 409, so there is no button.
	j.execSQL(`INSERT INTO job (id, kind, payload, status) VALUES ('stuck', 'judge', ?, 'queued')`, `{"take_id":"`+ids[1]+`","shot_id":"`+j.shotID+`"}`)
	assert.False(t, button(page(), ids[1]))
	assert.Equal(t, http.StatusConflict, j.form(http.MethodPost, "/takes/"+ids[1]+"/judge", nil).Code)
	j.execSQL(`DELETE FROM job WHERE id = 'stuck'`)

	// A Shot that is being generated: 409 too.
	j.execSQL(`UPDATE shot SET state = 'GENERATING' WHERE id = ?`, j.shotID)
	assert.False(t, button(page(), ids[1]))
	assert.Equal(t, http.StatusConflict, j.form(http.MethodPost, "/takes/"+ids[1]+"/judge", nil).Code)
	j.execSQL(`UPDATE shot SET state = 'PLANNING' WHERE id = ?`, j.shotID)

	// A Take without a video cannot be judged.
	var file string
	require.NoError(t, j.db.Reader().QueryRow(`SELECT file_path FROM take WHERE id = ?`, ids[1]).Scan(&file))
	j.execSQL(`UPDATE take SET file_path = NULL WHERE id = ?`, ids[1])
	assert.False(t, button(page(), ids[1]))
	assert.Equal(t, http.StatusConflict, j.form(http.MethodPost, "/takes/"+ids[1]+"/judge", nil).Code)
	j.execSQL(`UPDATE take SET file_path = ? WHERE id = ?`, file, ids[1])

	rec := j.form(http.MethodPost, "/takes/"+ids[1]+"/judge", nil)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.Eventually(t, func() bool { return j.results(ids[1]) == 1 }, 30*time.Second, 10*time.Millisecond)
	assert.False(t, button(page(), ids[1]), "judged again")
}

// Without the Judge POST /takes/:id/judge is always 503, so no card offers it.
func TestJudge_NoButtonWithoutTheJudge(t *testing.T) {
	g := newGenApp(t, true)
	require.Equal(t, http.StatusAccepted, g.form(http.MethodPost, "/shots/"+g.shotID+"/generate", nil).Code)
	g.waitTakes(3)
	html := g.get("/projects/"+g.project+"/scenes/"+g.sceneID+"?shot="+g.shotID, true).Body.String()
	assert.Contains(t, html, "/takes/", "the Takes are listed")
	assert.NotContains(t, html, "/judge\"")
}

func TestJudge_IsUnavailableWithoutFrameSampling(t *testing.T) {
	g := newGenApp(t, true) // the generation app has no Frames: the Judge is off
	assert.Equal(t, http.StatusServiceUnavailable, g.form(http.MethodPost, "/takes/anything/judge", nil).Code)

	require.Equal(t, http.StatusAccepted, g.form(http.MethodPost, "/shots/"+g.shotID+"/generate", nil).Code)
	g.waitTakes(3)
	var id string
	require.NoError(t, g.db.Reader().QueryRow(`SELECT id FROM take LIMIT 1`).Scan(&id))
	assert.Equal(t, http.StatusServiceUnavailable, g.form(http.MethodPost, "/takes/"+id+"/judge", nil).Code)
	assert.Equal(t, http.StatusNotFound, g.get("/api/v1/takes/"+id+"/judge-result", false).Code)
}

func TestJudge_TheShotInspectorShowsEachTakesLatestJudgeResultAndTheGenerationModel(t *testing.T) {
	const actingIssue = `{"result":"pass","overall_confidence":0.9,"issues":[{"type":"acting_drift","severity":"medium","dimension":"acting","time_range":"0.8-1.7s","description":"表情が急に変わる"}]}`
	j := newJudgeApp(t, actingIssue, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95)
	require.NoError(t, repository.NewLockRepository(j.db).SetShotLock(context.Background(), j.shotID, domain.ParamActing, domain.LockLocked, nil))
	before := j.get("/shots/"+j.shotID, false).Body.String()
	assert.NotContains(t, before, "data-judge-result")

	j.generateAndJudge()

	html := j.get("/shots/"+j.shotID, false).Body.String()
	assert.Contains(t, html, "data-judge-result")
	assert.Contains(t, html, "Judge: fail（確信度 90%）")
	assert.Contains(t, html, "LOCK 違反 / medium / acting_drift / 0.8-1.7s: 表情が急に変わる")
	assert.Contains(t, html, `aria-label="Generation Model"`)
	assert.NotContains(t, html, "自動選択（生成時に決定）", "the Shot has a Take, whose model is the Generation Model")
}
