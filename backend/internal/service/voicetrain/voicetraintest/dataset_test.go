package voicetraintest_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
	"github.com/ousiassllc/mam/backend/internal/service/voicetrain"
)

func cause(t *testing.T, err error) string {
	t.Helper()
	ae, ok := apperr.As(err)
	require.True(t, ok, "a refusal is the common error: %v", err)
	return ae.View(apperr.LangJa).Cause
}

func TestAddClip_KeepsTheRecordingWithItsTranscriptAndMeasuredLength(t *testing.T) {
	f := newFixture(t, "ok")
	f.reference()

	c, err := f.svc.AddClip(context.Background(), f.project, "c1", file("a.wav", wavOf(7)), "  こんにちは\nせかい  ")

	require.NoError(t, err)
	assert.Equal(t, "こんにちは せかい", c.Text, "a line break is a space; the ends are trimmed")
	assert.Equal(t, 7*time.Second, c.Duration())
	clips, err := f.svc.Clips(context.Background(), f.project, "c1")
	require.NoError(t, err)
	require.Len(t, clips, 1)
	assert.Equal(t, c, clips[0])
	path, err := f.svc.ClipPath(context.Background(), f.project, "c1", c.ID)
	require.NoError(t, err)
	assert.FileExists(t, path)
	assert.Contains(t, filepath.ToSlash(path), "/voicetrain/_draft/project-p1/c1/", "the draft is below the weights root but not a trained voice")
}

func TestAddClip_RefusesWhatTheTrainingCannotUseAndLeavesNothingOnDisk(t *testing.T) {
	long := strings.Repeat("あ", 501)
	for _, tc := range []struct {
		name  string
		file  voicerefsvc.File
		text  string
		cause string
	}{
		{"a format that is not audio", file("a.txt", []byte("hello")), "x", "のファイルを選んでください"},
		{"an extension that does not match the content", file("a.mp3", wavOf(5)), "x", "内容が一致しません"},
		{"an empty file", voicerefsvc.File{Name: "a.wav", Size: 0}, "x", "ファイルが空です"},
		{"a file over the size limit", voicerefsvc.File{Name: "a.wav", Size: voicetrain.MaxClipBytes + 1, Open: file("a.wav", wavOf(5)).Open}, "x", "MB までです"},
		{"a clip that is too short", file("a.wav", wavOf(1)), "x", "2〜30 秒"},
		{"a clip that is too long", file("a.wav", wavOf(31)), "x", "2〜30 秒"},
		{"no transcript", file("a.wav", wavOf(5)), "  ", "書き起こしを入力"},
		{"a transcript that is too long", file("a.wav", wavOf(5)), long, "500 文字以内"},
		{"a transcript with the separator of the list file", file("a.wav", wavOf(5)), "あ|い", "「|」"},
		{"a transcript with a control character", file("a.wav", wavOf(5)), "あ\x00い", "制御文字"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, "ok")
			f.reference()

			_, err := f.svc.AddClip(context.Background(), f.project, "c1", tc.file, tc.text)

			assert.Equal(t, apperr.CodeInvalidInput, failureCode(t, err))
			assert.Contains(t, cause(t, err), tc.cause)
			assert.Empty(t, f.files(filepath.Join(f.assets, "voicetrain", "_draft")), "nothing is kept")
		})
	}
}

func TestAddClip_NeedsTheReferenceVoiceAndTheCharacter(t *testing.T) {
	f := newFixture(t, "ok")

	_, err := f.svc.AddClip(context.Background(), f.project, "c1", file("a.wav", wavOf(5)), "x")
	assert.Equal(t, apperr.CodeConflict, failureCode(t, err), "a Character without a reference voice")
	assert.Contains(t, cause(t, err), "参照音声がありません")

	_, err = f.svc.AddClip(context.Background(), f.project, "ghost", file("a.wav", wavOf(5)), "x")
	assert.ErrorIs(t, err, voicetrain.ErrNotFound)
	_, err = f.svc.AddClip(context.Background(), voicerefsvc.ForProject("../etc"), "c1", file("a.wav", wavOf(5)), "x")
	assert.ErrorIs(t, err, voicetrain.ErrNotFound, "an id that is no directory name never reaches the disk")
	assert.Empty(t, f.files(f.assets))
}

func TestAddClip_StopsAtTheLimitsOfTheData(t *testing.T) {
	f := newFixture(t, "ok")
	f.reference()
	f.clips(voicetrain.MaxTotalSeconds/30, 30) // 30 minutes in 60 clips

	_, err := f.svc.AddClip(context.Background(), f.project, "c1", file("one-more.wav", wavOf(2)), "x")

	assert.Equal(t, apperr.CodeInvalidInput, failureCode(t, err))
	assert.Contains(t, cause(t, err), "合わせて 30 分まで")
	clips, _ := f.svc.Clips(context.Background(), f.project, "c1")
	assert.Len(t, clips, 60)

	g := newFixture(t, "ok")
	g.reference()
	for i := range voicetrain.MaxClips {
		_, err := g.svc.AddClip(context.Background(), g.project, "c1", file(fmt.Sprintf("c%d.wav", i), wavOf(2)), "x")
		require.NoError(t, err)
	}
	_, err = g.svc.AddClip(context.Background(), g.project, "c1", file("x.wav", wavOf(2)), "x")
	assert.Contains(t, cause(t, err), "200 個まで")
}

func TestAddClip_WithoutFFprobeNothingIsAccepted(t *testing.T) {
	f := newFixture(t, "ok")
	f.reference()
	svc := voicetrain.New(voicetrain.Deps{Voices: f.voices, Runtime: f.rt, AssetsDir: f.assets})

	_, err := svc.AddClip(context.Background(), f.project, "c1", file("a.wav", wavOf(5)), "x")

	assert.Equal(t, apperr.CodeConflict, failureCode(t, err))
	assert.Contains(t, cause(t, err), "ffprobe")
	assert.Empty(t, f.files(filepath.Join(f.assets, "voicetrain")))
}

func TestRemoveClips_DeletesOneOrAll(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, "ok")
	f.reference()
	f.clips(3, 10)
	clips, _ := f.svc.Clips(ctx, f.project, "c1")

	require.NoError(t, f.svc.RemoveClips(ctx, f.project, "c1", clips[1].ID))
	left, _ := f.svc.Clips(ctx, f.project, "c1")
	assert.Equal(t, []string{clips[0].ID, clips[2].ID}, []string{left[0].ID, left[1].ID})
	assert.NoFileExists(t, filepath.Join(f.assets, "voicetrain", "_draft", "project-p1", "c1", clips[1].File))
	assert.ErrorIs(t, f.svc.RemoveClips(ctx, f.project, "c1", clips[1].ID), voicetrain.ErrNotFound)

	require.NoError(t, f.svc.RemoveClips(ctx, f.project, "c1", ""))
	assert.Empty(t, f.files(filepath.Join(f.assets, "voicetrain")), "the draft directory is gone with its last clip")
	_, err := os.Stat(filepath.Join(f.assets, "voicetrain", "_draft", "project-p1"))
	assert.True(t, err == nil || os.IsNotExist(err))
}

func TestStart_TheClipsOfTheDraftAreCopiedSoTheDraftCanChangeMeanwhile(t *testing.T) {
	f := newFixture(t, "hang-sovits")
	f.ready()
	st, err := f.svc.Start(context.Background(), f.project, "c1", voicetrain.Options{EpochsSoVITS: 2, EpochsGPT: 1})
	require.NoError(t, err)
	require.Eventually(t, func() bool { _, err := os.Stat(filepath.Join(f.rt.work, st.ID, "hung.pid")); return err == nil }, 30*time.Second, 10*time.Millisecond)

	require.NoError(t, f.svc.RemoveClips(context.Background(), f.project, "c1", ""))

	assert.FileExists(t, filepath.Join(f.rt.work, st.ID, "data", "data.list"))
	list, err := os.ReadFile(filepath.Join(f.rt.work, st.ID, "data", "data.list"))
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(list)), "\n")
	require.Len(t, lines, 6)
	assert.Regexp(t, `^[A-Za-z0-9-]+\.wav\|mam\|ja\|これは 0 番目の音声です$`, lines[0], "the list of the vendor's scripts: file|speaker|language|text")
	require.NoError(t, f.svc.Cancel(f.project, "c1"))
}
