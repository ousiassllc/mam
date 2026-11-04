package availabilitytest_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
)

const loraPath = "/loras/" + "d.safetensors" // the URL path of mmtest.LoraD on the fake host

// catalogEntry is the catalog's answer alone (the workflow is not on the endpoint before an install).
func catalogEntry(t *testing.T, r *mmtest.Rig) mmview.Entry {
	t.Helper()
	v, err := r.Svc.Model(modelID)
	require.NoError(t, err)
	return mmview.EntryOf(r.Svc, v)
}

// #1025: fetching the optional file of an installed model is not an install of the model: the required files stay in place, so the model stays installed and
// usable (on both sides) for the whole download, and only carries the job.
func TestFetchingAnOptionalFileLeavesTheInstalledModelAvailable(t *testing.T) {
	r := installed(t)
	require.NoError(t, os.Remove(r.ModelFile(mmtest.LoraD)))
	hold := r.Host.Hold(loraPath)

	ref, err := r.Svc.InstallModel(context.Background(), modelID, nil)
	require.NoError(t, err)
	hold.Reached(t)

	entry, listed, direct := answers(t, r)
	assert.Equal(t, modelmanager.ModelInstalled, entry.State, "the required files are in place: still installed while the LoRA downloads")
	assert.True(t, entry.Available)
	assert.True(t, listed.Available, "a generation is not refused for the time of the download")
	assert.Equal(t, ref.JobID, entry.InstallJob, "the job is still shown")
	assert.Equal(t, []string{mmtest.LoraD.Name}, entry.MissingOptional)
	assertSame(t, entry, listed, direct)

	hold.Release()
	require.Equal(t, domain.JobSucceeded, r.Wait(ref.JobID).State)
	entry, _, _ = answers(t, r)
	assert.Equal(t, modelmanager.ModelInstalled, entry.State)
	assert.Empty(t, entry.InstallJob)
	assert.Empty(t, entry.MissingOptional)
}

// Installing again after the LoRA was deleted fetches that file alone: the required files are not downloaded again, the note is gone, the file is back.
func TestInstallingAgainFetchesOnlyTheMissingOptionalFile(t *testing.T) {
	r := installed(t)
	require.NoError(t, os.Remove(r.ModelFile(mmtest.LoraD)))
	entry, _, _ := answers(t, r)
	require.NotEmpty(t, entry.PartialNote)
	before := r.Host.Requests()

	st := r.InstallModel(modelID)
	require.Equal(t, domain.JobSucceeded, st.State, st.Error)

	assert.Equal(t, before+1, r.Host.Requests(), "one request: the LoRA; the required files are verified records and not fetched again")
	entry, listed, direct := answers(t, r)
	assert.Equal(t, modelmanager.ModelInstalled, entry.State)
	assert.Empty(t, entry.PartialNote)
	assert.Empty(t, entry.MissingOptional)
	assert.FileExists(t, r.ModelFile(mmtest.LoraD))
	assertSame(t, entry, listed, direct)
}

// The files are on disk without a record and the optional LoRA cannot be had (the host dropped it): installing records the model all the same, as the guidance
// says, and the LoRA is named as missing with a warning in the job. It is not a failure (#1025).
func TestAnOptionalFileThatCannotBeFetchedIsAWarningNotAFailure(t *testing.T) {
	r := mmtest.New(t, withOptionalModel())
	r.Start()
	r.InstallEngine()
	for _, f := range []mmtest.File{mmtest.ClipC, mmtest.VaeBC} {
		path := r.ModelFile(f)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, f.Body, 0o600))
	}
	r.Host.Gone(mmtest.LoraD)
	entry := catalogEntry(t, r)
	require.True(t, entry.FilesInPlace)
	assert.Contains(t, entry.Message, "任意のファイルは取得を試み、取れなくても導入済みにします", "the guidance says what the install does with the optional files")

	st := r.InstallModel(modelID)

	require.Equal(t, domain.JobSucceeded, st.State, st.Error)
	assert.Contains(t, st.Progress.Message, mmtest.LoraD.Name, "the warning names the file that was left out")
	entry, listed, direct := answers(t, r)
	assert.Equal(t, modelmanager.ModelInstalled, entry.State)
	assert.True(t, entry.Available)
	assert.Equal(t, []string{mmtest.LoraD.Name}, entry.MissingOptional)
	assert.Contains(t, entry.PartialNote, mmtest.LoraD.Name)
	assertSame(t, entry, listed, direct)
}

// A required file that cannot be fetched still fails the install: only an optional one is let go.
func TestARequiredFileThatCannotBeFetchedStillFailsTheInstall(t *testing.T) {
	r := mmtest.New(t, withOptionalModel())
	r.Start()
	r.InstallEngine()
	r.Host.Gone(mmtest.ClipC)

	st := r.InstallModel(modelID)

	assert.Equal(t, domain.JobFailed, st.State)
	assert.Equal(t, modelmanager.ModelNotInstalled, catalogEntry(t, r).State)
}
