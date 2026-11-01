// Package editortest builds the fixture the tests of the editing tools share: a Project in DIRECTOR mode
// with one Shot whose Take has a real video file (ffmpeg), the Editor Judge answered by a fake model and
// the audio vendors replaced by a tone. A test skips (fails in CI) without ffmpeg.
package editortest

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

// Fixture is the Env with the Project, its Shot and Take.
type Fixture struct {
	*mcptest.Env
	ProjectID string
	ShotID    string
	TakeID    string
	// Judge is the answer of the fake Editor Judge model (JSON); change it before a judge call.
	Judge *string
}

const passVerdict = `{"verdict":"pass","findings":[]}`

type fakeLLM struct{ answer *string }

func (l fakeLLM) Complete(_ context.Context, agent llm.Agent, _ llm.Request) (llm.Response, error) {
	if agent == llm.AgentAudioDirector {
		return llm.Response{Text: `{"scenes":[{"index":1,"emotion":"期待","bgm":"warm acoustic guitar","ambience":"","effects":[]}]}`}, nil
	}
	return llm.Response{Text: *l.answer}, nil
}

// tone speaks, composes and generates a 1.5 s tone.
type tone struct{ data []byte }

func (t tone) Synthesize(context.Context, domain.VoiceLine) (audiodirector.Speech, error) {
	return audiodirector.Speech{Data: t.data, Ext: "wav"}, nil
}

func (t tone) Compose(context.Context, string, float64) (audiodirector.Speech, error) {
	return audiodirector.Speech{Data: t.data, Ext: "wav"}, nil
}

func (t tone) Generate(context.Context, string, float64, bool) (audiodirector.Speech, error) {
	return audiodirector.Speech{Data: t.data, Ext: "wav"}, nil
}

// VoiceCloner answers every clone request with one voice.
type VoiceCloner struct{}

// Clone returns a fixed voice id.
func (VoiceCloner) Clone(context.Context, string, audiodirector.Sample) (string, error) {
	return "cloned-voice-1", nil
}

// New builds the Fixture and registers the tools with register. tweak changes the Deps after the fixture has set its own (a test of
// another provider of the audio, say).
func New(t *testing.T, register func(env *mcptest.Env, r *mcpkit.Registrar), tweak ...func(*service.Deps)) *Fixture {
	t.Helper()
	tools, err := media.Look()
	if err != nil {
		testenv.MissingFFmpeg(t, "ffmpeg / ffprobe are not installed")
	}
	testenv.Parallel(t)
	answer := passVerdict
	sound := tone{data: testenv.FFmpegFile(t, ".wav", "-f", "lavfi", "-i", "sine=frequency=300:duration=1.5")}
	env := mcptest.New(t, register, append([]func(*service.Deps){func(d *service.Deps) {
		d.LLM, d.Media = fakeLLM{answer: &answer}, &tools
		d.Audio = audiodirector.Providers{Speech: sound, Music: sound, Sounds: sound, Cloner: VoiceCloner{}}
	}}, tweak...)...)
	f := &Fixture{Env: env, Judge: &answer}
	ctx := context.Background()

	p, err := env.Services.Projects.Create(ctx, service.CreateProjectInput{Content: "美容液のCMを作りたい"})
	require.NoError(t, err)
	f.ProjectID = p.ID
	scene, err := env.Services.Plans.CreateScene(ctx, p.ID, service.SceneForm{Title: "Street"})
	require.NoError(t, err)
	shots, err := env.Services.Plans.CreateShot(ctx, scene.Scene.ID, service.ShotForm{Objective: "走り出す", DurationSeconds: "4"})
	require.NoError(t, err)
	f.ShotID = shots.Shots[len(shots.Shots)-1].ID
	f.TakeID = f.AddTake("take-1", 4)

	// DIRECTOR: the stages run one by one, as the tools call them. DRAMA asks nothing of a one-clip cut.
	// FULL_AUTO: no approval card stops a stage.
	f.Exec(`UPDATE project SET mode = 'director', production_template = 'drama', confirmation_level = 'full_auto', state = 'VISUAL_DESIGN' WHERE id = ?`, p.ID)
	_, err = env.Services.Takes.Select(ctx, f.TakeID)
	require.NoError(t, err)
	return f
}

// AddTake adds a Take of the Shot with a seconds-long video file.
func (f *Fixture) AddTake(id string, seconds int) string {
	f.T.Helper()
	rel := f.ProjectID + "/" + id + ".mp4"
	abs := filepath.Join(f.AssetsDir, filepath.FromSlash(rel))
	require.NoError(f.T, os.MkdirAll(filepath.Dir(abs), 0o700))
	require.NoError(f.T, os.WriteFile(abs, testenv.PatternVideo(f.T, "1920x1080", seconds), 0o600))
	f.Exec(`INSERT OR IGNORE INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot) VALUES ('pv-'||?, ?, 1, '{}')`, f.ShotID, f.ShotID)
	f.Exec(`INSERT INTO asset (id, project_id, type, file_path, mime_type) VALUES ('a-'||?, ?, 'video', ?, 'video/mp4')`, id, f.ProjectID, rel)
	f.Exec(`INSERT INTO take (id, prompt_version_id, model_name, model_parameters, file_path) VALUES (?, 'pv-'||?, 'kling-3.0', '{"resolution":"1080p"}', ?)`, id, f.ShotID, rel)
	return id
}

// Exec runs a statement against the database.
func (f *Fixture) Exec(query string, args ...any) {
	f.T.Helper()
	require.NoError(f.T, f.DB.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, query, args...)
		return err
	}))
}

// State is the state of the Project.
func (f *Fixture) State() string {
	f.T.Helper()
	var s string
	require.NoError(f.T, f.DB.Reader().QueryRow(`SELECT state FROM project WHERE id = ?`, f.ProjectID).Scan(&s))
	return s
}

// Editors is the repository of the editing stages, for the state a test needs to set up or check.
func (f *Fixture) Editors() *repository.EditorRepository { return repository.NewEditorRepository(f.DB) }

// WaitJob polls get_job until the job is over (or fails the test after a bound) and returns its status.
func (f *Fixture) WaitJob(jobID string) mcpkit.JobStatus {
	f.T.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		st, err := f.Registrar.Jobs.Get(context.Background(), jobID)
		require.NoError(f.T, err)
		if st.State.Final() {
			return st
		}
		require.True(f.T, time.Now().Before(deadline), "job %s did not end: %+v", jobID, st)
		time.Sleep(10 * time.Millisecond)
	}
}

// WithScript saves a script whose Scene has two spoken lines, so that the audio has speech to make.
func (f *Fixture) WithScript() {
	f.T.Helper()
	doc := domain.ScriptDoc{Script: domain.Script{Scenes: []domain.ScriptScene{{Title: "Street", Dialogue: []string{"ナレーション: 朝が来た", "ユイ: 行こう"}}}}}
	_, err := repository.NewPlanRepository(f.DB).SaveScript(context.Background(), f.ProjectID, doc)
	require.NoError(f.T, err)
}
