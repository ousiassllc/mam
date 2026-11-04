package facemodels_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/facesim"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/facemodels"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/facemodels/facemodelstest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

func exists(t testing.TB, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	return err == nil
}

// Issue #1026: from nothing installed, one install puts the two pinned files in the model folder and the engine reads them at once: no restart.
func TestInstallPutsThePinnedFilesInTheModelFolderAndTheEngineReadsThemWithoutARestart(t *testing.T) {
	r := facemodelstest.New(t)
	before := r.Svc.Status()
	assert.Equal(t, facemodels.StateMissing, before.State)
	require.NotNil(t, before.Off, "the engine is off while the files are not there")
	assert.Equal(t, facesim.ReasonModelsMissing, before.Off.Reason)
	assert.Equal(t, []string{facesim.DetectorFile, facesim.RecognizerFile}, before.Off.Missing)
	assert.Equal(t, int32(0), r.Loaded.Load())

	r.Install(t)

	for _, name := range []string{facesim.DetectorFile, facesim.RecognizerFile} {
		got, err := os.ReadFile(filepath.Join(r.Dir, name))
		require.NoError(t, err)
		assert.Equal(t, facemodelstest.Content(name), got)
	}
	after := r.Svc.Status()
	assert.Equal(t, facemodels.StateInstalled, after.State)
	assert.True(t, after.Installed())
	assert.Nil(t, after.Off, "the engine loads the installed models on its next use")
	assert.Equal(t, int32(1), r.Loaded.Load(), "loaded once, lazily")
	assert.True(t, facesim.Usable(r.Engine))
}

// The service does not fetch before the user agreed, and a second install fetches nothing.
func TestInstallNeedsTheAgreementAndFetchesNothingTwice(t *testing.T) {
	r := facemodelstest.New(t)
	err := r.Svc.Install(context.Background(), false, nil)
	require.ErrorIs(t, err, facemodels.ErrNotAccepted)
	assert.Equal(t, int32(0), r.Requests.Load())
	assert.False(t, exists(t, filepath.Join(r.Dir, facesim.DetectorFile)))

	r.Install(t)
	fetched := r.Requests.Load()
	require.Positive(t, fetched)
	r.Install(t)
	assert.Equal(t, fetched, r.Requests.Load(), "everything is in place and verified: nothing is downloaded again")
}

// A file that is not the pinned one is discarded: nothing is left in the model folder, and the user is told why.
func TestAChangedUpstreamFileIsRefusedAndNothingIsKept(t *testing.T) {
	r := facemodelstest.New(t)
	r.Corrupt.Store(true)

	err := r.Svc.Install(context.Background(), true, nil)

	var invalid *gterr.InvalidError
	require.ErrorAs(t, err, &invalid)
	assert.Contains(t, invalid.Message, "固定した値")
	assert.False(t, exists(t, filepath.Join(r.Dir, facesim.DetectorFile)))
	assert.False(t, exists(t, filepath.Join(r.Dir, facesim.RecognizerFile)))
	assert.Equal(t, facemodels.StateMissing, r.Svc.Status().State)
}

// Files put there by hand (or by scripts/fetch-face-models.sh) load all the same; the install only checks them against the pins, and keeps what is right.
func TestFilesPutByHandAreCheckedAndNotFetchedAgain(t *testing.T) {
	r := facemodelstest.New(t)
	require.NoError(t, os.MkdirAll(r.Dir, 0o750))
	for _, name := range []string{facesim.DetectorFile, facesim.RecognizerFile} {
		require.NoError(t, os.WriteFile(filepath.Join(r.Dir, name), facemodelstest.Content(name), 0o600))
	}
	assert.Equal(t, facemodels.StateUnverified, r.Svc.Status().State)
	assert.Nil(t, r.Svc.Status().Off, "the engine does not wait for the check")

	r.Install(t)

	assert.Equal(t, int32(0), r.Requests.Load(), "the files are the pinned ones: nothing is downloaded")
	assert.Equal(t, facemodels.StateInstalled, r.Svc.Status().State)
}

// Removal lets go of the engine first (Windows keeps a loaded model's file open), deletes the two files only, and the face similarity is off again.
func TestRemoveLetsGoOfTheEngineBeforeItDeletesAndKeepsTheOtherFiles(t *testing.T) {
	r := facemodelstest.New(t)
	r.Install(t)
	require.True(t, facesim.Usable(r.Engine), "the engine is loaded")
	other := filepath.Join(r.Dir, "my-own-model.onnx")
	require.NoError(t, os.WriteFile(other, []byte("mine"), 0o600))

	require.NoError(t, r.Svc.Remove())

	assert.Equal(t, int32(1), r.Closed.Load(), "the engine was closed")
	assert.Equal(t, int32(1), r.ClosedWithFiles.Load(), "...while the files were still there: nothing deleted a file the engine held")
	assert.False(t, exists(t, filepath.Join(r.Dir, facesim.DetectorFile)))
	assert.False(t, exists(t, filepath.Join(r.Dir, facesim.RecognizerFile)))
	assert.True(t, exists(t, other), "the user's own file stays")
	st := r.Svc.Status()
	assert.Equal(t, facemodels.StateMissing, st.State)
	require.NotNil(t, st.Off)
	assert.Equal(t, facesim.ReasonModelsMissing, st.Off.Reason)
	assert.False(t, facesim.Usable(r.Engine))

	require.NoError(t, r.Svc.Remove(), "models that are already gone are removed without an error")

	r.Install(t) // and they can be installed again without a restart
	assert.True(t, facesim.Usable(r.Engine))
	assert.Equal(t, int32(2), r.Loaded.Load())
}

// The ONNX Runtime library is another matter: installing the models does not turn the similarity on, and the status says which of the two is missing.
func TestAMissingONNXRuntimeIsNotFixedByTheModels(t *testing.T) {
	r := facemodelstest.New(t)
	r.RuntimeMissing.Store(true)

	r.Install(t)

	st := r.Svc.Status()
	assert.Equal(t, facemodels.StateInstalled, st.State, "the models are installed")
	require.NotNil(t, st.Off)
	assert.Equal(t, facesim.ReasonRuntimeMissing, st.Off.Reason)
	v := st.View()
	assert.True(t, v.Installed)
	assert.False(t, v.EngineReady)
	assert.Equal(t, facesim.ReasonRuntimeMissing, v.EngineOff)
	assert.Empty(t, v.InstallTool, "installing the models again is not the fix")

	r.RuntimeMissing.Store(false) // the library appears: the next use loads, no restart
	assert.True(t, facesim.Usable(r.Engine))
}

// A removal is refused while an install runs; the install is not hurt by it.
func TestRemoveIsRefusedWhileAnInstallRuns(t *testing.T) {
	r := facemodelstest.New(t)
	gate := make(chan struct{})
	r.Gate.Store(&gate)
	done := make(chan error, 1)
	go func() { done <- r.Svc.Install(context.Background(), true, nil) }()
	require.Eventually(t, func() bool { return r.Svc.Progress().Running }, 10*time.Second, time.Millisecond)

	var conflict *gterr.ConflictError
	require.ErrorAs(t, r.Svc.Remove(), &conflict)

	close(gate)
	require.NoError(t, <-done)
	assert.Equal(t, facemodels.StateInstalled, r.Svc.Status().State)
	assert.False(t, r.Svc.Progress().Running)
}

// The status carries what the user is shown before the install: sizes, sources, licenses with the answer, and the notice about the training data.
func TestTheStatusCarriesTheLicensesAndSources(t *testing.T) {
	r := facemodelstest.New(t)
	st := r.Svc.Status()
	require.Len(t, st.Files, 2)
	assert.Positive(t, st.DownloadBytes)
	assert.Equal(t, st.DownloadBytes, st.RemainBytes)
	require.Len(t, st.Licenses, 2)
	v := st.View()
	assert.Equal(t, facemodels.ToolName, v.InstallTool)
	assert.Equal(t, filepath.Clean(r.Dir), v.ModelDir)
	assert.False(t, v.EngineReady)
	assert.Equal(t, "models_missing", v.EngineOff)
	assert.NotEmpty(t, v.Files[0].SourceURL)
}
