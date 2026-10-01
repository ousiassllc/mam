package service_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// noKeySpeech is an ElevenLabs without its API key.
type noKeySpeech struct{}

func (noKeySpeech) Synthesize(context.Context, domain.VoiceLine) (audiodirector.Speech, error) {
	return audiodirector.Speech{}, audiodirector.ErrNoAPIKey
}

const autoStopNotice = "自動の編集工程が止まりました"

func (f genFixture) countNotices(t *testing.T) int {
	n := 0
	for _, m := range f.thread(t, f.projectID) {
		if m.Kind == domain.KindText && m.Role == domain.RoleAssistant && strings.Contains(m.Content, autoStopNotice) {
			n++
		}
	}
	return n
}

// autoAudioFixture is an AUTO Project whose Rough Cut passed the Editor Judge and waits in AUDIO
// for audio that cannot be made: the ElevenLabs key is not set.
func autoAudioFixture(t *testing.T) genFixture {
	t.Helper()
	f := autoFixture(t)
	f.selectTakeOf(t, f.shotID) // the last Selected Take starts the chain: Rough Cut, Editor Judge, audio
	require.Eventually(t, func() bool { return f.countNotices(t) == 1 }, 15*time.Second, 10*time.Millisecond, "the stage that cannot run is announced")
	require.Equal(t, domain.StateAudio, f.state(t))
	return f
}

// autoFixture is an AUTO Project at FULL AUTO whose only Shot has its Takes but no Selected Take, with
// the ElevenLabs key missing.
func autoFixture(t *testing.T) genFixture {
	t.Helper()
	f := newGenFixtureWithDeps(t, true, func(string) (uint64, error) { return 1 << 40, nil }, func(d *service.Deps) {
		// Takes of the fake models are not video files: ffprobe is a script that finds 4 seconds in anything.
		bin := t.TempDir()
		for _, name := range []string{"ffmpeg", "ffprobe"} {
			script := "#!/bin/sh\necho '{\"format\":{\"duration\":\"4\"},\"streams\":[{\"codec_type\":\"video\",\"width\":1280,\"height\":720,\"r_frame_rate\":\"24\"}]}'\n"
			require.NoError(t, os.WriteFile(filepath.Join(bin, name), []byte(script), 0o700)) // #nosec G306 -- a test executable
		}
		t.Setenv("PATH", bin)
		tools, err := media.Look()
		require.NoError(t, err)
		d.Media = &tools
		d.Audio = audiodirector.Providers{Speech: noKeySpeech{}}
	})
	f.llm.extractor = extractAnswer
	f.llm.editor = `{"verdict":"pass","findings":[]}`
	f.buildReferencePack(t)
	f.generateAndWait(t, f.shotID, 2)
	f.execSQL(t, `UPDATE project SET mode = 'auto', production_template = 'drama', confirmation_level = 'full_auto', state = 'VISUAL_DESIGN' WHERE id = ?`, f.projectID)
	return f
}

func TestAdvanceAuto_AFailedStageIsNotRetriedOrAnnouncedAgainByUnrelatedUpdates(t *testing.T) {
	f := autoAudioFixture(t)
	planned := f.llm.calls(llm.AgentAudioDirector)

	for range 5 { // every job update of any Project wakes the chain
		f.svc.Editor.ResumeEditing()
		time.Sleep(50 * time.Millisecond)
	}

	assert.Equal(t, 1, f.countNotices(t), "the same stop is announced once")
	assert.Equal(t, planned, f.llm.calls(llm.AgentAudioDirector), "the Audio Director is not paid again")
}

func TestAdvanceAuto_TheUsersAnswerRunsTheFailedStageAgain(t *testing.T) {
	f := autoAudioFixture(t)

	f.svc.Editor.RetryAuto(f.projectID) // what an answered Approval card does

	require.Eventually(t, func() bool { return f.countNotices(t) == 2 }, 15*time.Second, 10*time.Millisecond)
}

func TestEditorClose_CancelsARunningAutoStageInsteadOfWaitingForIt(t *testing.T) {
	f := autoFixture(t)
	entered := make(chan struct{})
	f.llm.editorHold = func(ctx context.Context) error {
		close(entered)
		<-ctx.Done() // the Editor Judge goes on until it is told to stop
		return ctx.Err()
	}
	f.selectTakeOf(t, f.shotID)
	select {
	case <-entered:
	case <-time.After(15 * time.Second):
		t.Fatal("the chain never reached the Editor Judge")
	}

	closed := make(chan struct{})
	go func() { f.svc.Close(); close(closed) }()

	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close waits for the AUTO stage")
	}
	assert.Zero(t, f.countNotices(t), "a stage cut short by the shutdown is not reported as a failure")
}

func TestEditorClose_AnAnswerAfterCloseStartsNothing(t *testing.T) {
	f := autoAudioFixture(t)
	f.svc.Close()

	f.svc.Editor.RetryAuto(f.projectID)

	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, 1, f.countNotices(t), "the chain does not run after Close")
}
