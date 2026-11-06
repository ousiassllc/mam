package audiodirector_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

func tools(t *testing.T) media.Tools {
	t.Helper()
	tools, err := media.Look()
	if err != nil {
		testenv.MissingFFmpeg(t, "ffmpeg / ffprobe are not installed")
	}
	return tools
}

// tone is an mp3 tone of the given length.
func tone(t *testing.T, seconds float64) []byte {
	t.Helper()
	ffmpeg := testenv.FFmpegPath(t)
	out := filepath.Join(t.TempDir(), "tone.mp3")
	cmd := exec.Command(ffmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=300:duration=%g", seconds), "-af", "volume=0.3", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make tone: %v: %s", err, b)
	}
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	return data
}

// testVideo writes a silent video of the given length.
func testVideo(t *testing.T, seconds float64) string {
	t.Helper()
	ffmpeg := testenv.FFmpegPath(t)
	out := filepath.Join(t.TempDir(), "take.mp4")
	cmd := exec.Command(ffmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", fmt.Sprintf("color=c=blue:s=160x90:r=10:d=%g", seconds), "-pix_fmt", "yuv420p", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make video: %v: %s", err, b)
	}
	return out
}

// fakeVendors is every provider of the Audio Director in one fake: each call returns a tone and is
// remembered.
type fakeVendors struct {
	t  *testing.T
	mu sync.Mutex

	lines   []domain.VoiceLine
	music   []string
	sounds  []string
	loops   []bool
	syncs   []audiodirector.LipSyncRequest
	failMu  bool // the music call fails
	failSFX bool
	// unreadable makes the speech, music and sound calls answer with bytes that are not audio, so
	// the local work after a paid call (measuring the file) fails.
	unreadable bool
}

var notMedia = []byte("this is not audio")

func (f *fakeVendors) Synthesize(_ context.Context, line domain.VoiceLine) (audiodirector.Speech, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lines = append(f.lines, line)
	if f.unreadable {
		return audiodirector.Speech{Data: notMedia, Ext: "mp3"}, nil
	}
	return audiodirector.Speech{Data: tone(f.t, 1), Ext: "mp3"}, nil
}

func (f *fakeVendors) Compose(_ context.Context, prompt string, seconds float64) (audiodirector.Speech, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.music = append(f.music, prompt)
	if f.failMu {
		return audiodirector.Speech{}, &audiodirector.ProviderError{Status: 500, Message: "boom"}
	}
	if f.unreadable {
		return audiodirector.Speech{Data: notMedia, Ext: "mp3"}, nil
	}
	return audiodirector.Speech{Data: tone(f.t, seconds), Ext: "mp3"}, nil
}

func (f *fakeVendors) Generate(_ context.Context, prompt string, seconds float64, loop bool) (audiodirector.Speech, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sounds = append(f.sounds, prompt)
	f.loops = append(f.loops, loop)
	if f.failSFX {
		return audiodirector.Speech{}, &audiodirector.ProviderError{Status: 500, Message: "boom"}
	}
	if f.unreadable {
		return audiodirector.Speech{Data: notMedia, Ext: "mp3"}, nil
	}
	return audiodirector.Speech{Data: tone(f.t, seconds), Ext: "mp3"}, nil
}

func (f *fakeVendors) Sync(_ context.Context, req audiodirector.LipSyncRequest) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.syncs = append(f.syncs, req)
	return req.Video.Data, nil // the clip itself is a valid video
}

func (f *fakeVendors) providers() audiodirector.Providers {
	return audiodirector.Providers{Speech: f, Music: f, Sounds: f, LipSync: f}
}

// fakeLedger is an in-memory cost ledger with a budget. Like the repository, it counts what is
// reserved next to what is recorded.
type fakeLedger struct {
	mu       sync.Mutex
	ceiling  *float64
	spent    float64
	reserved float64
	entries  []domain.CostEntry
}

func (l *fakeLedger) ProjectBudget(context.Context, string) (*float64, float64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ceiling, l.spent + l.reserved, nil
}

func (l *fakeLedger) Reserve(_ context.Context, _ string, cost float64) (func(), bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.ceiling != nil && *l.ceiling-l.spent-l.reserved < cost-1e-9 {
		return nil, false, nil
	}
	l.reserved += cost
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.reserved -= cost
		})
	}, true, nil
}

func (l *fakeLedger) AddCost(_ context.Context, e domain.CostEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, e)
	l.spent += e.EstimatedCost
	return nil
}

func (l *fakeLedger) providers() []domain.ModelName {
	out := make([]domain.ModelName, len(l.entries))
	for i, e := range l.entries {
		out[i] = e.Provider
	}
	return out
}

// fakePlanner answers the Planner's model call with a fixed text.
type fakePlanner struct {
	answer string
	err    error
	calls  int
	req    llm.Request
	agent  llm.Agent
}

func (f *fakePlanner) Complete(_ context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
	f.calls++
	f.req, f.agent = req, agent
	return llm.Response{Text: f.answer}, f.err
}

// twoScenes is a cut of 10 seconds: a fast first Scene (4 clips in 4 s) and a slow second one (1 clip in 6 s).
func twoScenes() []audiodirector.Scene {
	return []audiodirector.Scene{
		{Index: 1, Title: "出会い", Location: "駅前の広場", Start: 0, End: 4, Clips: 4, Lines: []string{"美咲: こんにちは"}},
		{Index: 2, Title: "別れ", Start: 4, End: 10, Clips: 1, Lines: []string{"ナレーション: 夕暮れ"}},
	}
}

func newDirector(t *testing.T, v *fakeVendors, ledger audiodirector.Ledger, planner *audiodirector.Planner) (*audiodirector.Director, string) {
	t.Helper()
	root := t.TempDir()
	return audiodirector.New(audiodirector.Deps{Providers: v.providers(), Planner: planner, Ledger: ledger, Tools: tools(t), Root: root}), root
}

func skippedLayers(tl domain.AudioTimeline, reason string) []domain.AudioLayer {
	var out []domain.AudioLayer
	for _, s := range tl.Skipped {
		if s.Reason == reason {
			out = append(out, s.Layer)
		}
	}
	return out
}
