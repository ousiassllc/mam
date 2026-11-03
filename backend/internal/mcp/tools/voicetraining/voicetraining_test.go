package voicetraining_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/editortest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/voicereference"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/voicetraining"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/worldtest"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/voicetrain"
)

// TestMain makes the test binary the fake driver of the fine-tuning when MAM_FAKE_TRAIN is set (the training starts os.Args[0] with the step and
// the job file): it reports epochs and writes the two weights, as the real driver does.
func TestMain(m *testing.M) {
	if mode := os.Getenv("MAM_FAKE_TRAIN"); mode != "" {
		os.Exit(fakeDriver(mode, os.Args[1], os.Args[2]))
	}
	os.Exit(m.Run())
}

func fakeDriver(mode, step, jobPath string) int {
	raw, err := os.ReadFile(jobPath)
	if err != nil {
		return 3
	}
	var j struct {
		Exp, Out, Work string
		EpochsS2       int `json:"epochs_sovits"`
		EpochsS1       int `json:"epochs_gpt"`
		Clips          int
	}
	_ = json.Unmarshal(raw, &struct {
		Exp  *string `json:"exp_dir"`
		Out  *string `json:"out_dir"`
		Work *string `json:"work_dir"`
	}{&j.Exp, &j.Out, &j.Work})
	_ = json.Unmarshal(raw, &j)
	_ = os.MkdirAll(j.Exp, 0o700)
	_ = os.MkdirAll(j.Out, 0o700)
	progress := func(done, total int) {
		fmt.Printf("MAM_TRAIN {\"step\": %q, \"done\": %d, \"total\": %d}\n", step, done, total)
	}
	switch step {
	case "hubert":
		progress(j.Clips, j.Clips)
	case "sovits", "gpt":
		epochs, name := j.EpochsS2, "sovits.pth"
		if step == "gpt" {
			epochs, name = j.EpochsS1, "gpt.ckpt"
		}
		for n := 1; n <= epochs; n++ {
			progress(n, epochs)
			if mode == "hang" {
				_ = os.WriteFile(filepath.Join(j.Work, "hung"), nil, 0o600)
				select {}
			}
		}
		if mode == "fail" {
			fmt.Fprintln(os.Stderr, "RuntimeError: CUDA out of memory. Tried to allocate 2.00 GiB")
			return 1
		}
		_ = os.WriteFile(filepath.Join(j.Out, name), []byte("weights of "+step), 0o600)
	}
	return 0
}

type held struct{ rt *runtime }

func (h held) Spec(_ string, args, _ []string) (proc.Spec, error) {
	return proc.Spec{Command: os.Args[0], Args: args, Dir: h.rt.work, Env: []string{"MAM_FAKE_TRAIN=" + h.rt.mode}}, nil
}
func (h held) WorkDir() string    { return h.rt.work }
func (h held) GPU() (string, int) { return "NVIDIA GeForce RTX 4070", 12 }
func (h held) End()               { h.rt.ended.Add(1) }

type runtime struct {
	mode, work string
	refuse     error
	ended      atomic.Int32
}

func (r *runtime) BeginTool(context.Context, string, ...string) (voicetrain.Held, error) {
	if r.refuse != nil {
		return nil, r.refuse
	}
	return held{r}, nil
}

type probe struct{}

func (probe) AudioDuration(_ context.Context, path string) (float64, error) {
	b, err := os.ReadFile(path)
	if err != nil || len(b) < 46 {
		return 0, fmt.Errorf("not audio")
	}
	return float64(binary.LittleEndian.Uint16(b[44:46])), nil
}

// wavOf is a wav that says its length in seconds in its first sample.
func wavOf(seconds int) []byte {
	var b bytes.Buffer
	data := make([]byte, 64)
	binary.LittleEndian.PutUint16(data, uint16(seconds)) //nolint:gosec // a test length
	b.WriteString("RIFF\x24\x00\x00\x00WAVEfmt ")
	b.Write(make([]byte, 20)) // the size of the fmt chunk and its body
	b.WriteString("data\x40\x00\x00\x00")
	b.Write(data)
	return b.Bytes()
}

type fixture struct {
	env *mcptest.Env
	p   worldtest.Project
	rt  *runtime
}

func newFixture(t *testing.T, mode string) *fixture {
	t.Helper()
	f := &fixture{rt: &runtime{mode: mode, work: filepath.Join(t.TempDir(), "train")}}
	f.env = mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		env.Services.VoiceTrain = voicetrain.New(voicetrain.Deps{
			Voices: env.Services.VoiceRef, Runtime: f.rt, Probe: probe{}, AssetsDir: env.AssetsDir,
			FreeBytes: func(string) (uint64, error) { return 1 << 40, nil }, Stop: 300 * time.Millisecond,
		})
		t.Cleanup(env.Services.VoiceTrain.Close)
		jobs.Register(r, env.Services) // get_job
		voicereference.Register(r, env.Services)
		voicetraining.Register(r, env.Services)
	}, mcptest.WithJobQueue)
	f.p = worldtest.Seed(t, f.env)
	return f
}

func (f *fixture) target() map[string]string {
	return map[string]string{"project_id": f.p.ID, "character_id": "c1"}
}

func (f *fixture) reference() {
	f.env.T.Helper()
	params := f.target()
	params["text"] = "おはよう"
	rec := editortest.Upload(f.env, "character_voice_reference", params, "ref.wav", wavOf(5))
	require.Equal(f.env.T, http.StatusCreated, rec.Code, rec.Body.String())
}

func (f *fixture) clip(name string, seconds int, text string) *httptest.ResponseRecorder {
	params := f.target()
	params["text"] = text
	return editortest.Upload(f.env, voicetraining.PurposeUpload, params, name, wavOf(seconds))
}

func (f *fixture) clips(n, seconds int) {
	f.env.T.Helper()
	for i := range n {
		rec := f.clip(fmt.Sprintf("c%d.wav", i), seconds, fmt.Sprintf("これは %d 番目", i))
		require.Equal(f.env.T, http.StatusCreated, rec.Code, rec.Body.String())
	}
}

func (f *fixture) view() voicetraining.View {
	var v voicetraining.View
	f.env.CallOK("get_character_voice_training", f.target(), &v)
	return v
}

func (f *fixture) start(args map[string]any) voicetraining.StartOutput {
	a := map[string]any{"project_id": f.p.ID, "character_id": "c1"}
	for k, v := range args {
		a[k] = v
	}
	var out voicetraining.StartOutput
	f.env.CallOK("start_character_voice_training", a, &out)
	return out
}

func TestTraining_ClipsAreUploadedCountedAndRemoved(t *testing.T) {
	f := newFixture(t, "ok")
	f.reference()

	empty := f.view()
	assert.True(t, empty.HasReference)
	assert.Empty(t, empty.Clips)
	require.Len(t, empty.Problems, 1, "why a training cannot start yet")
	assert.Equal(t, 8, empty.Defaults.SoVITS)
	assert.Equal(t, 25, empty.MaxEpochs.SoVITS)
	assert.Equal(t, 60, empty.Limits["min_total_seconds"])
	assert.Contains(t, empty.Consent, "本人の同意")

	rec := f.clip("a.wav", 10, "こんにちは")
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "/voicetrain", "the stored file is not named")
	f.clips(5, 10)
	v := f.view()
	assert.Len(t, v.Clips, 6)
	assert.InDelta(t, 60, v.DataSeconds, 0.01)
	assert.Empty(t, v.Problems)

	var after voicetraining.View
	f.env.CallOK("delete_character_voice_training_clips", map[string]any{"project_id": f.p.ID, "character_id": "c1", "clip_id": v.Clips[0].ID}, &after)
	assert.Len(t, after.Clips, 5)
	assert.Contains(t, f.env.CallError("delete_character_voice_training_clips", map[string]any{"project_id": f.p.ID, "character_id": "c1", "clip_id": v.Clips[0].ID}), "not_found")
	f.env.CallOK("delete_character_voice_training_clips", f.target(), &after)
	assert.Empty(t, after.Clips)
}

func TestTraining_RefusesWhatTheScreenRefuses(t *testing.T) {
	f := newFixture(t, "ok")

	assert.Contains(t, f.env.CallError("get_character_voice_training", map[string]any{"character_id": "c1"}), "series_id か project_id")
	assert.Contains(t, f.env.CallError("get_character_voice_training", map[string]any{"project_id": f.p.ID, "series_id": "s", "character_id": "c1"}), "series_id か project_id")
	assert.Contains(t, f.env.CallError("get_character_voice_training", map[string]any{"project_id": f.p.ID, "character_id": "ghost"}), "not_found")
	_, err := f.env.Registrar.Files.UploadTicketFor(t.Context(), voicetraining.PurposeUpload, map[string]string{"project_id": f.p.ID, "character_id": "c1"})
	require.Error(t, err, "the transcript is required")

	assert.Equal(t, http.StatusConflict, f.clip("a.wav", 10, "x").Code, "no reference voice yet")
	f.reference()
	assert.Equal(t, http.StatusUnprocessableEntity, f.clip("a.wav", 1, "x").Code, "a clip that is too short")
	assert.Equal(t, http.StatusUnprocessableEntity, f.clip("a.txt", 10, "x").Code, "not audio")

	// Not enough data, and no path or command is a parameter of any tool.
	msg := f.env.CallError("start_character_voice_training", map[string]any{"project_id": f.p.ID, "character_id": "c1"})
	assert.Contains(t, msg, "学習に使う音声がありません")
	f.clips(6, 10)
	assert.Contains(t, f.env.CallError("start_character_voice_training", map[string]any{"project_id": f.p.ID, "character_id": "c1", "epochs_gpt": 99}), "GPT のエポック数")
}

func TestTraining_TheRuntimeThatIsNotThereRefusesWithTheReasonAndNoJobIsMade(t *testing.T) {
	f := newFixture(t, "ok")
	f.rt.refuse = localruntime.ErrNotInstalled
	f.reference()
	f.clips(6, 10)

	msg := f.env.CallError("start_character_voice_training", f.target())
	assert.Contains(t, msg, "GPT-SoVITS")
	assert.Contains(t, msg, string(apperr.ClassConflict), "the common error of #1010")
	assert.Nil(t, f.view().Current)
}
