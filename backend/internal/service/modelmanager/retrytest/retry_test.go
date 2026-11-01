// Package retrytest tests that an install resumes by itself when the connection to the download host breaks, and what it logs.
package retrytest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

func TestMain(m *testing.M) { mmtest.Main(m) }

// logs is the JSON log of the Service, readable while it is written.
type logs struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

// records are the log lines of the level with the message; the install of the engine (target "engine") is left out.
func (l *logs) records(t *testing.T, level, msg string) []map[string]any {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(l.buf.Bytes()), []byte("\n")) {
		var rec map[string]any
		if json.Unmarshal(line, &rec) == nil && rec["level"] == level && rec["msg"] == msg && rec["target"] != "engine" {
			out = append(out, rec)
		}
	}
	return out
}

func newRig(t *testing.T, retry fetch.Retry) (*mmtest.Rig, *logs) {
	t.Helper()
	l := &logs{}
	r := mmtest.New(t, mmtest.WithLogger(slog.New(slog.NewJSONHandler(l, nil))), mmtest.FetchRetry(retry))
	r.InstallEngine()
	return r, l
}

func requireFile(t *testing.T, r *mmtest.Rig, f mmtest.File) {
	t.Helper()
	got, err := os.ReadFile(r.ModelFile(f))
	require.NoError(t, err)
	assert.Equal(t, f.Body, got)
	assert.NoFileExists(t, r.ModelFile(f)+".part")
}

// The connection to the host is cut twice in the middle of the first file: the install resumes from the part file each time,
// finishes, and logs the resumes (WARN) and the success (INFO) with the job and the model.
func TestAnInstallResumesByItselfWhenTheConnectionIsCut(t *testing.T) {
	r, l := newRig(t, fetch.Retry{Backoff: 1, MaxBackoff: 1})
	r.Host.Drop(mmtest.UnetB, 100_000, 2)
	before := r.Host.Requests()

	st := r.InstallModel("m-b")

	require.Equal(t, domain.JobSucceeded, st.State, st.Error)
	requireFile(t, r, mmtest.UnetB)
	requireFile(t, r, mmtest.VaeBC)
	assert.Equal(t, 4, r.Host.Requests()-before, "two cut transfers, the transfer that finished the first file, and the second file")

	resumes := l.records(t, "WARN", "download interrupted, resuming from the part file")
	require.Len(t, resumes, 2)
	for i, rec := range resumes {
		assert.Equal(t, st.JobID, rec["job_id"])
		assert.Equal(t, "model:m-b", rec["target"])
		assert.EqualValues(t, i+1, rec["attempt"])
		assert.NotEmpty(t, rec["err"])
	}
	done := l.records(t, "INFO", "install succeeded")
	require.Len(t, done, 1)
	assert.Equal(t, st.JobID, done[0]["job_id"])
	assert.Equal(t, "model:m-b", done[0]["target"])
	assert.Empty(t, l.records(t, "ERROR", "install failed"))
}

// When every transfer is cut, the job fails with the message that says what to do; the part file stays, and installing again
// continues from it and succeeds. The failure is logged (ERROR) with the job, the model and the cause.
func TestAnInstallThatKeepsBreakingFailsWithAClearMessageAndResumesNextTime(t *testing.T) {
	r, l := newRig(t, fetch.Retry{Attempts: 3, Backoff: 1, MaxBackoff: 1})
	r.Host.Drop(mmtest.UnetB, 50_000, 100)

	st := r.InstallModel("m-b")

	require.Equal(t, domain.JobFailed, st.State)
	assert.Equal(t, "通信が切れました。もう一度導入すると続きから再開します", st.Error)
	assert.NoFileExists(t, r.ModelFile(mmtest.UnetB))
	part, err := os.Stat(r.ModelFile(mmtest.UnetB) + ".part")
	require.NoError(t, err, "the download so far stays")
	assert.Positive(t, part.Size())

	failed := l.records(t, "ERROR", "install failed")
	require.Len(t, failed, 1)
	assert.Equal(t, st.JobID, failed[0]["job_id"])
	assert.Equal(t, "model:m-b", failed[0]["target"])
	assert.Contains(t, failed[0]["err"], "the connection was lost (3 transfers)")
	assert.Len(t, l.records(t, "WARN", "download interrupted, resuming from the part file"), 2, "three transfers: two resumes")
	assert.Empty(t, l.records(t, "INFO", "install succeeded"))

	r.Host.Drop(mmtest.UnetB, 0, 0) // the network is back
	st = r.InstallModel("m-b")
	require.Equal(t, domain.JobSucceeded, st.State, st.Error)
	requireFile(t, r, mmtest.UnetB)
	requireFile(t, r, mmtest.VaeBC)
	assert.Len(t, l.records(t, "INFO", "install succeeded"), 1)
}

// A file that is gone is told as gone, at once: nothing resumes, and the failure is logged like any other.
func TestAnInstallOfAGoneFileIsNotRetried(t *testing.T) {
	r, l := newRig(t, fetch.Retry{})
	r.Host.Gone(mmtest.UnetB)

	st := r.InstallModel("m-b")

	require.Equal(t, domain.JobFailed, st.State)
	assert.Contains(t, st.Error, "取得元が消えています")
	assert.Empty(t, l.records(t, "WARN", "download interrupted, resuming from the part file"))
	failed := l.records(t, "ERROR", "install failed")
	require.Len(t, failed, 1)
	assert.Equal(t, st.JobID, failed[0]["job_id"])
}

// A stop the user asked for is not retried, is logged as INFO (not an error) with the job and the model, and leaves the part file.
func TestAnInstallTheUserStopsIsNotRetried(t *testing.T) {
	r, l := newRig(t, fetch.Retry{Backoff: 1, MaxBackoff: 1})
	hold := r.Host.Hold("/diffusion_models/b.safetensors")
	ref, err := r.Svc.InstallModel(context.Background(), "m-b", nil)
	require.NoError(t, err)
	r.Start()
	hold.Reached(t)

	require.NoError(t, r.Svc.CancelInstall(ref.JobID))
	st := r.Wait(ref.JobID)

	require.Equal(t, domain.JobFailed, st.State)
	assert.Contains(t, st.Error, "導入を中断しました")
	assert.Empty(t, l.records(t, "WARN", "download interrupted, resuming from the part file"))
	assert.Empty(t, l.records(t, "ERROR", "install failed"))
	stopped := l.records(t, "INFO", "install cancelled by the user")
	require.Len(t, stopped, 1)
	assert.Equal(t, ref.JobID, stopped[0]["job_id"])
	assert.Equal(t, "model:m-b", stopped[0]["target"])
}
