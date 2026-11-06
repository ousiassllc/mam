// Package jobview holds what the handlers of the model manager screen share when they draw a job: a size as the screen
// writes it and the progress of a running install. It is a package of its own because the handler package and its
// child packages (hfimport) both need it and a child cannot import its parent.
package jobview

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/failbox"
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

// ModelVRAMText is the guideline of the video memory of a catalog model: the minimum, and the vendor's recommendation above it when there is
// one (a GPU between the two runs the model with limits), with where it comes from (the source of a model with a recommendation says what
// is reported and what is measured, so it is shown whole, also for an estimate).
func ModelVRAMText(v catalog.VRAM) string {
	if v.RecommendedGB > v.MinGB {
		return fmt.Sprintf("%d GB 以上（推奨 %d GB 以上）。出典: %s", v.MinGB, v.RecommendedGB, v.Source)
	}
	return VRAMText(v.MinGB, v.Source, v.Estimate)
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

// Ended is how a job ended, as a notice or a failure; both are empty while it runs (or when it is unknown). The failure carries the
// common error (#1010) the job kept, which the screen draws as the error box.
func Ended(ctx context.Context, svc *modelmanager.Service, jobID, name string) (notice string, failure failbox.Item) {
	st, err := svc.JobStatus(ctx, jobID)
	if err != nil {
		return "", failbox.Item{}
	}
	return Ending(st.State, st.Error, st.Failure, name+"を導入しました", name+"の導入に失敗しました")
}

// Ending is how an install job ended, from its state, the reason it failed with and the common error it kept: the notice of a success (done) or
// the failure titled title. Both are empty while the job runs.
func Ending(state domain.JobStatus, reason string, common *apperr.View, done, title string) (notice string, failure failbox.Item) {
	switch state {
	case domain.JobSucceeded:
		return done, failbox.Item{}
	case domain.JobFailed:
		if reason = strings.TrimSpace(reason); reason == "" {
			reason = "原因は mam のログを確認してください"
		}
		return "", failbox.Item{Title: title, Reason: reason, View: common}
	}
	return "", failbox.Item{}
}

// Settle adds how a job ended to the message of its section: the notice of a success (joined with the others by "。"), and the failure as an
// error box when the job kept the common error, else as the line "<title>: <reason>" in Error (one per line). A job that has not ended adds nothing.
func Settle(m *view.Message, notice string, f failbox.Item) {
	if notice != "" {
		m.Notice = strings.Join(slices.DeleteFunc([]string{m.Notice, notice}, func(s string) bool { return s == "" }), "。")
	}
	switch {
	case f.Title == "":
	case f.View != nil:
		m.Failures = append(m.Failures, f)
	default:
		m.Error = strings.Join(slices.DeleteFunc([]string{m.Error, f.Title + ": " + f.Reason}, func(s string) bool { return s == "" }), "\n")
	}
}
