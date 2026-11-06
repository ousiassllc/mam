package jobview_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/jobview"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/failbox"
)

// A job that has not ended says nothing; a success is a notice; a failure carries the line it was told with and, when the job kept it,
// the common error (#1010).
func TestEnding(t *testing.T) {
	for _, s := range []domain.JobStatus{domain.JobQueued, domain.JobRunning} {
		n, f := jobview.Ending(s, "", nil, "done", "failed")
		assert.Empty(t, n)
		assert.Equal(t, failbox.Item{}, f, s)
	}
	n, f := jobview.Ending(domain.JobSucceeded, "", nil, "done", "failed")
	assert.Equal(t, "done", n)
	assert.Equal(t, failbox.Item{}, f)

	v := apperr.New(apperr.CodeDiskFull).View(apperr.LangJa)
	n, f = jobview.Ending(domain.JobFailed, "  no space  ", &v, "done", "failed")
	assert.Empty(t, n)
	assert.Equal(t, failbox.Item{Title: "failed", Reason: "no space", View: &v}, f)

	_, f = jobview.Ending(domain.JobFailed, "", nil, "done", "failed")
	assert.Equal(t, "原因は mam のログを確認してください", f.Reason, "a job with no reason points at the log")
}

// A success adds its notice (joined with the others by "。"); a failure with the common error is a box under its title, one without (a job of
// an older version) is the line it always was, one per line; a job that has not ended adds nothing.
func TestSettle(t *testing.T) {
	v := apperr.New(apperr.CodeVRAM).View(apperr.LangJa)
	var m view.Message
	jobview.Settle(&m, "", failbox.Item{})
	jobview.Settle(&m, "A を導入しました", failbox.Item{})
	jobview.Settle(&m, "B を導入しました", failbox.Item{})
	jobview.Settle(&m, "", failbox.Item{Title: "C の導入に失敗しました", Reason: "boom", View: &v})
	jobview.Settle(&m, "", failbox.Item{Title: "D の導入に失敗しました", Reason: "bang"})
	jobview.Settle(&m, "", failbox.Item{Title: "E の導入に失敗しました", Reason: "bing"})

	assert.Equal(t, "A を導入しました。B を導入しました", m.Notice)
	assert.Equal(t, "D の導入に失敗しました: bang\nE の導入に失敗しました: bing", m.Error)
	if assert.Len(t, m.Failures, 1) {
		assert.Equal(t, "C の導入に失敗しました", m.Failures[0].Title)
	}
}
