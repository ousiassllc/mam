package failreport_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
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

// Line is the sentence of a failure that no handler worded: the cause and the remedy of the common error, and for a failure nobody knows the error ID of
// its log line. The original message is never on the screen: it is in the log, for a known failure too.
func TestLine_IsTheCauseAndTheRemedyNotTheRawText(t *testing.T) {
	log, logs := logger()

	assert.Empty(t, failreport.Line(log, "x", nil))

	got := failreport.Line(log, "x", errors.New("boom: raw english text"))
	assert.True(t, strings.HasPrefix(got, sentenceOf(apperr.CodeUnknown)), "the sentence of the unknown failure")
	assert.NotContains(t, got, "boom", "the original message is not on the screen")
	id := regexp.MustCompile(`err_[0-9a-f-]+`).FindString(got)
	require.NotEmpty(t, id, got)
	assert.Contains(t, logs.String(), "error_id="+id)
	assert.Contains(t, logs.String(), "boom: raw english text", "the original message is in the log")

	logs.Reset()
	known := failreport.Line(log, "x", media.ErrUnavailable)
	assert.Equal(t, sentenceOf(apperr.CodeFFmpegMissing), known, "a known failure is its sentence, with no error ID")
	assert.Contains(t, logs.String(), "code="+string(apperr.CodeFFmpegMissing), "and its cause is in the log")
	assert.NotContains(t, logs.String(), "error_id=")

	logs.Reset()
	_ = failreport.Line(log, "x", context.Canceled)
	assert.Empty(t, logs.String(), "a caller that went away is not logged")
}

// The log line carries the original message with its secrets hidden (the file handler writes what it is given; only the reader hides a secret), for a known
// failure (WARN) and for an unknown one (ERROR with the error ID) and for a failed ffmpeg run.
func TestTheLogLinesDoNotCarrySecrets(t *testing.T) {
	const secret = "hf_abcdefghijklmnop1234"
	raw := func(what string) error {
		return errors.New(what + " https://h.example/x?key=SECRETVALUE12345 Authorization: Bearer " + secret)
	}

	log, logs := logger()
	_ = failreport.Line(log, "x", raw("boom"))
	assert.Contains(t, logs.String(), "boom", "the unknown failure: the original message is in the log")
	_ = failreport.Line(log, "x", fmt.Errorf("%w: %w", media.ErrUnavailable, raw("known")))
	assert.Contains(t, logs.String(), "known", "the known failure: the original message is in the log")
	_, _ = failreport.Media(log, "what.export", raw("ffmpeg"))
	assert.Contains(t, logs.String(), "ffmpeg", "the failed run: the original message is in the log")

	assert.NotContains(t, logs.String(), "SECRETVALUE12345")
	assert.NotContains(t, logs.String(), secret)
	assert.NotContains(t, logs.String(), "Bearer hf_", "the credential of a header")
}

func sentenceOf(code apperr.Code) string {
	return apperr.New(code).View(apperr.LangJa).Sentence(apperr.LangJa)
}
