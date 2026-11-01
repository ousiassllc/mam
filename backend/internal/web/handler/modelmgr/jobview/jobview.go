// Package jobview holds what the handlers of the model manager screen share when they draw a job: a size as the screen
// writes it and the progress of a running install. It is a package of its own because the handler package and its
// child packages (hfimport) both need it and a child cannot import its parent.
package jobview

import (
	"context"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
)

// BytesText is a size as the screen writes it: GiB with a decimal, or whole MiB (KiB below one MiB).
func BytesText(n int64) string {
	const gib = 1 << 30
	if n >= gib {
		return fmt.Sprintf("%.1f GiB", float64(n)/gib)
	}
	if n >= 1<<20 {
		return fmt.Sprintf("%.0f MiB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%.0f KiB", float64(n)/(1<<10))
}

// VRAMText is the guideline of the video memory with where it comes from.
func VRAMText(minGB int, source string, estimate bool) string {
	if estimate {
		return fmt.Sprintf("%d GB 以上（mam の推定。GPU では未確認）", minGB)
	}
	return fmt.Sprintf("%d GB 以上（出典: %s）", minGB, source)
}

// Progress is what a running job reports (a placeholder while its status cannot be read).
func Progress(ctx context.Context, svc *modelmanager.Service, jobID string) *view.Progress {
	st, err := svc.JobStatus(ctx, jobID)
	if err != nil {
		return &view.Progress{JobID: jobID, Message: "状態を取得しています"}
	}
	p := &view.Progress{JobID: jobID, Percent: st.Progress.Percent, Message: st.Progress.Message}
	if st.State == domain.JobQueued {
		p.Message = "順番を待っています"
	}
	if p.Message == "" {
		p.Message = "準備しています"
	}
	if t := st.Progress.TotalBytes; t > 0 {
		p.Bytes = BytesText(st.Progress.DoneBytes) + " / " + BytesText(t)
	}
	return p
}

// Ended is how a job ended, as a notice or an error line; both are empty while it runs (or when it is unknown).
func Ended(ctx context.Context, svc *modelmanager.Service, jobID, name string) (notice, failure string) {
	st, err := svc.JobStatus(ctx, jobID)
	if err != nil {
		return "", ""
	}
	switch st.State {
	case domain.JobSucceeded:
		return name + "を導入しました", ""
	case domain.JobFailed:
		reason := st.Error
		if reason == "" {
			reason = "原因は mam のログを確認してください"
		}
		return "", name + "の導入に失敗しました: " + reason
	}
	return "", ""
}
