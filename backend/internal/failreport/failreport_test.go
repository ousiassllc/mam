package failreport_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/failreport"
	"github.com/ousiassllc/mam/backend/internal/media"
)

func logger() (*slog.Logger, *bytes.Buffer) {
	var logs bytes.Buffer
	return slog.New(slog.NewTextHandler(&logs, nil)), &logs
}

// A failure of ffmpeg is told as the common error whatever the work was (the Rough Cut, the export, composing a Take, the audio mix): a Japanese
// cause that names the work and a remedy, never the English message of ffmpeg, which is the detail. What is about the machine keeps its own code.
func TestMedia_TellsTheWorkAndTheRemedyInJapanese(t *testing.T) {
	for name, tc := range map[string]struct {
		what string
		err  error
		code apperr.Code
		want []string
	}{
		"rough cut":  {"what.rough_cut", errors.New("ffmpeg: exit status 1"), apperr.CodeMediaFailed, []string{"Rough Cut の書き出しに失敗しました", "壊れていないか"}},
		"export":     {"what.export", errors.New("ffmpeg: exit status 1"), apperr.CodeMediaFailed, []string{"動画の書き出しに失敗しました", "空き容量"}},
		"compose":    {"what.compose", errors.New("ffmpeg: exit status 1"), apperr.CodeMediaFailed, []string{"Take の合成に失敗しました"}},
		"audio mix":  {"what.audio_mix", errors.New("ffmpeg: exit status 1"), apperr.CodeMediaFailed, []string{"音声のミキシングに失敗しました"}},
		"no ffmpeg":  {"what.export", media.ErrUnavailable, apperr.CodeFFmpegMissing, []string{"ffmpeg と ffprobe が見つかりません", "モデルマネージャーの「付随ソフト」"}},
		"time limit": {"what.export", fmt.Errorf("%w after 2m", media.ErrTimeout), apperr.CodeMediaTimeout, []string{"制限時間を超えました"}},
		"disk full":  {"what.compose", fmt.Errorf("write: %w", syscall.ENOSPC), apperr.CodeDiskFull, []string{"ディスクの空き容量が足りません"}},
	} {
		t.Run(name, func(t *testing.T) {
			log, _ := logger()

			e, reason := failreport.Media(log, tc.what, tc.err)

			assert.Equal(t, tc.code, e.Code)
			assert.ErrorIs(t, e, tc.err, "the failure behind stays reachable")
			for _, w := range tc.want {
				assert.Contains(t, reason, w)
			}
		})
	}
}

// A failure of ffmpeg that has no known cause gets an error ID, and the log line that says why has the same ID; a failure that has a known cause
// is not logged here, and a caller that went away is not a failure.
func TestMedia_AnUnknownCauseIsLoggedUnderTheErrorIDOfTheSentence(t *testing.T) {
	log, logs := logger()

	e, reason := failreport.Media(log, "what.export", errors.New("ffmpeg: Invalid data found"))
	require.NotEmpty(t, e.ID)
	assert.Contains(t, reason, "エラー ID: "+e.ID)
	assert.Contains(t, reason, "Invalid data found", "ffmpeg's own words, the start of the detail")
	assert.Contains(t, logs.String(), "error_id="+e.ID)

	logs.Reset()
	_, _ = failreport.Media(log, "what.export", media.ErrUnavailable)
	_, _ = failreport.Media(log, "what.export", context.Canceled)
	assert.Empty(t, logs.String())
}
