package judge

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// FailureReason is the user-facing cause of a failed Judge run. Details stay in the log.
func FailureReason(err error) string {
	if errors.Is(err, ErrNoFFmpeg) {
		return "ffmpeg / ffprobe が見つかりません（インストールして PATH に追加してください）"
	}
	return generation.FailureReason(err)
}

// Summary is the chat notice of a Judge result: the verdict and the Issues.
func Summary(shotNumber int, takeID string, res domain.JudgeResult) string {
	var b strings.Builder
	verdict := "PASS"
	if res.Result == domain.JudgeFail {
		verdict = "FAIL"
	}
	fmt.Fprintf(&b, "Shot %d · Take %s の Judge 結果: %s（信頼度 %.2f）", shotNumber, domain.ShortID(takeID), verdict, res.OverallConfidence)
	for _, is := range res.Issues {
		fmt.Fprintf(&b, "\n・[%s] %s", is.Severity, is.Type)
		if is.TimeRange != "" {
			b.WriteString(" " + is.TimeRange)
		}
		if is.LockViolation {
			b.WriteString("（LOCK 違反）")
		}
	}
	switch res.Metrics.FaceIdentityStatus {
	case domain.FaceUnavailable:
		b.WriteString("\n顔の同一性の数値評価は利用できないため、定性評価のみで判定しました。")
	case domain.FaceMeasured:
		var uncalibrated, undetected bool
		for _, f := range res.Metrics.FaceIdentity {
			switch {
			case !f.Calibrated:
				uncalibrated = true
			case !f.Evaluated:
				undetected = true
			}
		}
		if uncalibrated {
			b.WriteString("\n参照画像が足りず（顔が取れた参照が 2 枚未満、または参照同士が似ていない）顔の同一性の数値評価を行えないため、定性評価のみで判定しました。")
		}
		if undetected {
			b.WriteString("\n顔を検出できないフレームが多いため、顔の同一性は定性評価のみで判定しました。")
		}
	}
	if rs := res.RepairStrategy; rs != nil {
		fmt.Fprintf(&b, "\n推奨する修復: %s", rs.Action)
	}
	return b.String()
}
