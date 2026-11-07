package ollamapull_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/ollamamodels"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/ollamapull"
	"github.com/ousiassllc/mam/backend/internal/service/ollamapull/ollamatest"
)

const (
	layerA = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	layerB = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	layerC = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

// tiny is a model of 1000 bytes (the weights 900, a license 10, the config 90) so that the numbers of a stream are easy to read.
func tiny() ollamamodels.Model {
	return ollamamodels.Model{
		Name: "tiny:1b", DisplayName: "Tiny 1B", Description: "d", LicenseSummary: "s", CheckedAt: "2026-11-07", ConfigBytes: 90, TotalBytes: 1000,
		Layers: []ollamamodels.Layer{{Kind: "model", Digest: layerA, Size: 900}, {Kind: "license", Digest: layerB, Size: 10}},
	}
}

// successLines is the stream of a pull that works: the model layer, then the verification and the manifest.
func successLines() []ollamatest.Line {
	return []ollamatest.Line{
		{"status": "pulling manifest"},
		ollamatest.Status("pulling aaaaaaaaaaaa", layerA, 900, 0),
		ollamatest.Status("pulling aaaaaaaaaaaa", layerA, 900, 450),
		ollamatest.Status("pulling aaaaaaaaaaaa", layerA, 900, 900),
		ollamatest.Status("pulling bbbbbbbbbbbb", layerB, 10, 10),
		{"status": "verifying sha256 digest"},
		{"status": "writing manifest"},
		{"status": "success"},
	}
}

func pull(t *testing.T, r *ollamatest.Rig, name string) ollamapull.JobStatus {
	t.Helper()
	ref, err := r.Svc.Pull(context.Background(), name)
	require.NoError(t, err)
	return r.Wait(ref.JobID)
}

// A name that is not one of the list is refused before anything is asked of Ollama: another namespace, a tag that names a cloud model, a form that is no name, a name the catalog does not have.
func TestOnlyTheNamesOfTheListAreFetched(t *testing.T) {
	r := ollamatest.New(t)
	for _, bad := range []string{
		"qwen3.8:27b-cloud", "gpt-oss:120b-cloud", "huihui_ai/qwen3.8:27b", "hf.co/org/repo:Q4_K_M", "Qwen3.8:27B", "qwen3.8", "", "gemma4:12b\nx", "llama3.2:3b",
	} {
		var invalid *ollamapull.InvalidError
		_, err := r.Svc.Pull(context.Background(), bad)
		assert.ErrorAs(t, err, &invalid, "pull %q", bad)
		assert.ErrorAs(t, r.Svc.Delete(context.Background(), bad), &invalid, "delete %q", bad)
	}
	assert.Empty(t, r.Fake.Requests(), "Ollama was asked nothing")
	assert.Empty(t, r.Runtime.Calls(), "the runtime was not started")
}

// The list has exactly the three models of the catalog, in its order.
func TestTheListIsTheThreeModelsOfTheCatalog(t *testing.T) {
	r := ollamatest.New(t)
	var names []string
	for _, m := range r.Svc.List(context.Background()).Models {
		names = append(names, m.Model.Name)
		assert.Equal(t, ollamapull.StateMissing, m.State)
	}
	assert.Equal(t, []string{"gemma4:12b", "qwen3.8:27b", "deepseek-r1:14b"}, names)
}

// A server that is not installed, cannot run here or is being installed is the refusal of the runtime manager; a stopped one is no refusal.
func TestAPullIsRefusedWhenTheServerIsNotThere(t *testing.T) {
	r := ollamatest.New(t)
	ctx := context.Background()

	r.Runtime.Installed = false
	_, err := r.Svc.Pull(ctx, "gemma4:12b")
	assert.ErrorIs(t, err, localruntime.ErrNotInstalled)
	for _, m := range r.Svc.List(ctx).Models {
		assert.Equal(t, ollamapull.StateUnavailable, m.State)
	}

	r.Runtime.Installed, r.Runtime.Supported, r.Runtime.Reason = true, false, "AMD の GPU は未対応です"
	_, err = r.Svc.Pull(ctx, "gemma4:12b")
	var support *localruntime.SupportError
	require.ErrorAs(t, err, &support)
	assert.ErrorIs(t, err, localruntime.ErrUnsupported)
	assert.Contains(t, err.Error(), "AMD の GPU は未対応です")

	r.Runtime.Supported, r.Runtime.InstallJob = true, "job-1"
	_, err = r.Svc.Pull(ctx, "gemma4:12b")
	assert.ErrorIs(t, err, localruntime.ErrBusy)

	assert.Empty(t, r.Fake.Requests())
	assert.Empty(t, r.Runtime.Calls())
}

// The disk needs the total of the model and 1 GiB: one byte less is refused with the amount, the free space and the place.
func TestAPullNeedsTheTotalAndOneGiBOnTheDisk(t *testing.T) {
	r := ollamatest.New(t)
	need := uint64(8021618941) + 1<<30
	r.SetFree(need - 1)

	_, err := r.Svc.Pull(context.Background(), "gemma4:12b")

	var noSpace *fetch.NoSpaceError
	require.ErrorAs(t, err, &noSpace)
	assert.Equal(t, need, noSpace.Need)
	assert.Equal(t, need-1, noSpace.Free)
	assert.Equal(t, r.Layout.ModelsDir(), noSpace.Path)
	assert.Empty(t, r.Fake.Requests())

	r.SetFree(need)
	_, err = r.Svc.Pull(context.Background(), "gemma4:12b")
	assert.NoError(t, err)
}

// A pull on a stopped server starts it first (the job does), asks Ollama for the model of the name, and leaves the server running; the progress ends at 100 and the model is "installed" (verified).
func TestAPullStartsAStoppedServerAndIsVerifiedWhenTheManifestIsTheRecordedOne(t *testing.T) {
	r := ollamatest.New(t, ollamatest.WithModels(tiny()))
	r.Fake.Lines = successLines()

	st := pull(t, r, "tiny:1b")

	require.Equal(t, domain.JobSucceeded, st.State, st.Error)
	assert.Equal(t, "ollama:tiny:1b", st.Target)
	assert.Equal(t, 100, st.Progress.Percent)
	assert.Equal(t, []string{"tiny:1b"}, r.Fake.Pulls())
	assert.Equal(t, []string{"prepare"}, r.Runtime.Calls(), "started for the pull, not stopped after it")
	list := r.Svc.List(context.Background())
	assert.Equal(t, ollamapull.StateInstalled, list.Models[0].State)
	assert.Equal(t, ollamapull.CheckVerified, list.Models[0].Check)
}

// While the stream runs, the progress is the finished layers and the part of the layer in progress over the total of the record; it does not go back when the next layer begins.
func TestTheProgressOfARunningPullIsTheFinishedLayersAndTheCurrentOne(t *testing.T) {
	r := ollamatest.New(t, ollamatest.WithModels(tiny()))
	r.Fake.Lines = []ollamatest.Line{
		ollamatest.Status("pulling aaaaaaaaaaaa", layerA, 900, 450),
		ollamatest.Status("pulling aaaaaaaaaaaa", layerA, 900, 900),
		ollamatest.Status("pulling bbbbbbbbbbbb", layerB, 10, 0), // the next layer begins at 0
		ollamatest.Status("pulling bbbbbbbbbbbb", layerB, 10, 5),
		{"status": "success"},
	}
	release := r.Fake.HoldAt(3) // after three lines
	t.Cleanup(release)
	ref, err := r.Svc.Pull(context.Background(), "tiny:1b")
	require.NoError(t, err)
	r.Start()
	r.Fake.Reached()

	require.Eventually(t, func() bool {
		st, err := r.Svc.JobStatus(context.Background(), ref.JobID)
		return err == nil && st.Progress.Percent == 90
	}, 10*time.Second, 10*time.Millisecond, "(900 + 0) / 1000 and no less")
	st, _ := r.Svc.JobStatus(context.Background(), ref.JobID)
	assert.Equal(t, "pulling", st.Progress.Phase)
	assert.Contains(t, st.Progress.Message, "データを取得しています（90%")
	assert.Equal(t, int64(1000), st.Progress.TotalBytes)
	assert.Equal(t, int64(900), st.Progress.DoneBytes)
	assert.Equal(t, ollamapull.StatePulling, r.Svc.List(context.Background()).Models[0].State)
	assert.Equal(t, ref.JobID, r.Svc.List(context.Background()).Models[0].PullJob)

	release()
	assert.Equal(t, domain.JobSucceeded, r.Wait(ref.JobID).State)
}

// The same model asked for twice while it is being pulled is the same job.
func TestTheSameModelPulledTwiceIsOneJob(t *testing.T) {
	r := ollamatest.New(t, ollamatest.WithModels(tiny()))
	r.Fake.Lines = successLines()
	release := r.Fake.HoldAt(1)
	t.Cleanup(release)
	first, err := r.Svc.Pull(context.Background(), "tiny:1b")
	require.NoError(t, err)
	r.Start()
	r.Fake.Reached()

	second, err := r.Svc.Pull(context.Background(), "tiny:1b")

	require.NoError(t, err)
	assert.True(t, second.Existing)
	assert.Equal(t, first.JobID, second.JobID)
	release()
	assert.Equal(t, domain.JobSucceeded, r.Wait(first.JobID).State)
	assert.Equal(t, []string{"tiny:1b"}, r.Fake.Pulls())
}

// A line with an error is a failure of the job in words, with what Ollama said; the model is not installed.
func TestAnErrorLineFailsThePullInWords(t *testing.T) {
	r := ollamatest.New(t, ollamatest.WithModels(tiny()))
	r.Fake.Lines = []ollamatest.Line{
		{"status": "pulling manifest"},
		ollamatest.Status("pulling aaaaaaaaaaaa", layerA, 900, 100),
		{"error": "write /models/blobs/sha256-aaa: no space left on device"},
	}

	st := pull(t, r, "tiny:1b")

	require.Equal(t, domain.JobFailed, st.State)
	assert.Contains(t, st.Error, "ディスクの空きが足りません")
	assert.Contains(t, st.Error, "no space left on device")
	assert.Equal(t, ollamapull.StateMissing, r.Svc.List(context.Background()).Models[0].State)
}

// A stream that ends without its success line, and an answer that is no 200, are failures in words too.
func TestAStreamWithoutSuccessAndANon200AreFailures(t *testing.T) {
	r := ollamatest.New(t, ollamatest.WithModels(tiny()))
	r.Fake.Lines = []ollamatest.Line{{"status": "pulling manifest"}}
	st := pull(t, r, "tiny:1b")
	require.Equal(t, domain.JobFailed, st.State)
	assert.Contains(t, st.Error, "success の行がありません")
}

// A server that cannot be started fails the job with the end of its log.
func TestAServerThatDoesNotStartFailsThePullWithItsLog(t *testing.T) {
	r := ollamatest.New(t, ollamatest.WithModels(tiny()))
	r.Runtime.PrepareErr, r.Runtime.LogTail = errors.New("exit status 1"), "Error: listen tcp 127.0.0.1:1: bind: address already in use"

	st := pull(t, r, "tiny:1b")

	require.Equal(t, domain.JobFailed, st.State)
	assert.Contains(t, st.Error, "Ollama を起動できませんでした")
	assert.Contains(t, st.Error, "address already in use")
	assert.Empty(t, r.Fake.Pulls())
}

// Cancelling closes the connection to /api/pull; the job ends as stopped (not as an error of the network), and pulling again is possible.
func TestCancellingAPullClosesTheConnectionAndEndsTheJobAsStopped(t *testing.T) {
	r := ollamatest.New(t, ollamatest.WithModels(tiny()))
	r.Fake.Lines = successLines()
	release := r.Fake.HoldAt(2)
	t.Cleanup(release)
	ref, err := r.Svc.Pull(context.Background(), "tiny:1b")
	require.NoError(t, err)
	r.Start()
	r.Fake.Reached()

	require.NoError(t, r.Svc.Cancel(ref.JobID))
	st := r.Wait(ref.JobID)

	require.Equal(t, domain.JobFailed, st.State)
	assert.Contains(t, st.Error, "取得を中断しました")
	assert.Equal(t, ollamapull.StateMissing, r.Svc.List(context.Background()).Models[0].State)
	assert.ErrorIs(t, r.Svc.Cancel(ref.JobID), modelmanager.ErrNotFound, "a job that ended cannot be cancelled")
	assert.ErrorIs(t, r.Svc.Cancel("nope"), modelmanager.ErrNotFound)
}

// Pulling names the models whose pull is queued or running (the Ollama of mam is held by them, localruntime.Deps.HeldBy) and is empty before and after it.
func TestPullingNamesTheModelsBeingFetched(t *testing.T) {
	r := ollamatest.New(t, ollamatest.WithModels(tiny()))
	r.Fake.Lines = successLines()
	release := r.Fake.HoldAt(1)
	t.Cleanup(release)
	assert.Empty(t, r.Svc.Pulling())
	ref, err := r.Svc.Pull(context.Background(), "tiny:1b")
	require.NoError(t, err)
	assert.Equal(t, []string{"tiny:1b"}, r.Svc.Pulling(), "queued is held already")
	assert.Equal(t, "tiny:1b", r.Svc.HeldBy("ollama"))
	assert.Empty(t, r.Svc.HeldBy("gpt-sovits"), "no other runtime is held by a pull")
	r.Start()
	r.Fake.Reached()
	assert.Equal(t, []string{"tiny:1b"}, r.Svc.Pulling())

	release()
	require.Equal(t, domain.JobSucceeded, r.Wait(ref.JobID).State)
	require.Eventually(t, func() bool { return len(r.Svc.Pulling()) == 0 }, 10*time.Second, 10*time.Millisecond)
}
