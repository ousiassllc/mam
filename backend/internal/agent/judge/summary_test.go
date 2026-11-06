package judge_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/judge"
)

func TestFailureReason_MissingFFmpegPointsToTheSettingsDownloadAndThePathSetting(t *testing.T) {
	for _, err := range []error{judge.ErrNoFFmpeg, fmt.Errorf("sample frames: %w", judge.ErrNoFFmpeg)} {
		reason := judge.FailureReason(err)
		assert.Contains(t, reason, "ffmpeg / ffprobe が見つかりません")
		assert.Contains(t, reason, "設定画面", "the Settings screen is where ffmpeg is set up")
		assert.Contains(t, reason, "ダウンロード", "ffmpeg can be downloaded from Settings")
		assert.Contains(t, reason, "パス", "the path can be given in Settings")
	}
}
