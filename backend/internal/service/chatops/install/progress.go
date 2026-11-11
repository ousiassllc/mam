package install

import (
	"context"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
)

// JobProgress is how far an install job is, as the service that owns it reports (modelmanager.JobStatus, localruntime.JobStatus).
type JobProgress struct {
	State domain.JobStatus
	// Percent is 0-100; Phase is "download", "extract", "verify" ...; Message is what the job says it is doing.
	Percent int
	Phase   string
	Message string
	// DoneBytes and TotalBytes are the bytes of a download or an extraction (0 when not a transfer).
	DoneBytes, TotalBytes int64
	// Error is why a failed job failed.
	Error string
}

// JobSource reads a job; ok is false for a job it does not know (another service's job, or none).
type JobSource func(ctx context.Context, jobID string) (JobProgress, bool)

// Detail is the ProgressDetail of the install cards: it asks the sources in order, and answers "" for a job none of them knows, so that the next provider can answer.
func Detail(sources ...JobSource) chatops.ProgressDetail {
	return func(ctx context.Context, jobID string) string {
		for _, source := range sources {
			if p, ok := source(ctx, jobID); ok {
				return Describe(p)
			}
		}
		return ""
	}
}

var phases = map[string]string{"download": "ダウンロード中", "extract": "展開中", "verify": "検証中", "register": "登録中"}

// maxErrorRunes bounds the reason a failed job shows on the card.
const maxErrorRunes = 200

// Describe is the line under a progress card: "42% ・ダウンロード中（1.2 / 3.4 GB）".
func Describe(p JobProgress) string {
	switch p.State {
	case domain.JobQueued:
		return "順番待ち（まもなく始まります）"
	case domain.JobSucceeded:
		return "導入が完了しました"
	case domain.JobFailed:
		return "導入に失敗しました: " + shorten(p.Error)
	}
	step := phases[p.Phase]
	if step == "" {
		step = p.Message
	}
	if step == "" {
		step = "準備中"
	}
	out := fmt.Sprintf("%d%% ・%s", p.Percent, step)
	if p.TotalBytes > 0 {
		out += "（" + transfer(p.DoneBytes, p.TotalBytes) + "）"
	}
	return out
}

// transfer is "1.2 / 3.4 GB": both sizes in the unit of the total.
func transfer(done, total int64) string {
	const mb, gb = 1 << 20, 1 << 30
	if total >= gb {
		return fmt.Sprintf("%.1f / %.1f GB", float64(done)/gb, float64(total)/gb)
	}
	return fmt.Sprintf("%.0f / %.0f MB", float64(done)/mb, float64(total)/mb)
}

func shorten(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > maxErrorRunes {
		return string(r[:maxErrorRunes]) + "…"
	}
	if s == "" {
		return "ログを確かめてください"
	}
	return s
}

// runtimeRefusal tells an error of a local runtime as the MCP tool does.
func runtimeRefusal(err error) error {
	if _, msg := mmview.RefuseRuntime(err); msg != "" {
		return chatops.Refuse("%s", msg)
	}
	return err
}
