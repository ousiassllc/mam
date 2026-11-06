package ollamapull_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/ollamapull"
	"github.com/ousiassllc/mam/backend/internal/service/ollamapull/ollamatest"
)

// A manifest whose layer of the weights is another one (the tag was moved) is "changed": the model stays, it works, and nothing is deleted. A manifest that cannot be read is "changed" too.
func TestAMovedTagOrAnUnreadableManifestIsChangedAndNothingIsDeleted(t *testing.T) {
	for name, manifest := range map[string]func(string) any{
		"another digest": func(string) any { return ollamatest.ManifestOf("gemma4:12b", layerC) },
		"another size": func(string) any {
			m := ollamatest.ManifestOf("gemma4:12b", "")
			m["layers"].([]any)[1].(map[string]any)["size"] = 1
			return m
		},
		"no layer of the weights": func(string) any { return map[string]any{"layers": []any{}} },
		"not a manifest":          func(string) any { return "<html>moved</html>" },
	} {
		t.Run(name, func(t *testing.T) {
			r := ollamatest.New(t)
			r.Fake.Lines = []ollamatest.Line{{"status": "pulling manifest"}, {"status": "success"}}
			r.Fake.Manifest = manifest

			st := pull(t, r, "gemma4:12b")

			require.Equal(t, domain.JobSucceeded, st.State, "the pull itself worked")
			m := r.Svc.List(context.Background()).Models[0]
			assert.Equal(t, ollamapull.StateChanged, m.State)
			assert.Equal(t, ollamapull.CheckChanged, m.Check)
			assert.Empty(t, r.Fake.Deletes())
			r.Runtime.SetState(proc.StateStopped)
			assert.Equal(t, []string{"gemma4:12b"}, r.Svc.Names(context.Background()), "it is still a model to use")
		})
	}
}

// Delete: a model chosen by an Agent is refused with their names; one that is being pulled or is not fetched is a conflict; a name outside the list is refused; none of them touches Ollama.
func TestDeleteRefusesAModelInUseOrBeingPulledOrNotFetched(t *testing.T) {
	r := ollamatest.New(t)
	r.Fake.Lines = []ollamatest.Line{{"status": "success"}}
	ctx := context.Background()

	assert.ErrorIs(t, r.Svc.Delete(ctx, "gemma4:12b"), modelmanager.ErrNotInstalled, "not fetched")

	pull(t, r, "gemma4:12b")
	r.Use("gemma4:12b", "Writer", "Director")
	err := r.Svc.Delete(ctx, "gemma4:12b")
	var invalid *ollamapull.InvalidError
	require.ErrorAs(t, err, &invalid)
	assert.Contains(t, invalid.Message, "Writer、Director")
	assert.Empty(t, r.Fake.Deletes(), "nothing was removed")
	assert.Equal(t, ollamapull.StateInstalled, r.Svc.List(ctx).Models[0].State)
}

func TestDeleteRefusesAModelThatIsBeingPulled(t *testing.T) {
	r := ollamatest.New(t, ollamatest.WithModels(tiny()))
	r.Fake.Lines = successLines()
	release := r.Fake.HoldAt(1)
	t.Cleanup(release)
	ref, err := r.Svc.Pull(context.Background(), "tiny:1b")
	require.NoError(t, err)
	r.Start()
	r.Fake.Reached()

	assert.ErrorIs(t, r.Svc.Delete(context.Background(), "tiny:1b"), modelmanager.ErrBusy)

	release()
	r.Wait(ref.JobID)
}

// Delete asks Ollama (DELETE /api/delete) and the model is gone; a server that was stopped runs for the delete only, a running one keeps running.
func TestDeleteAsksOllamaAndStartsAStoppedServerOnlyForIt(t *testing.T) {
	for _, running := range []bool{false, true} {
		t.Run(map[bool]string{false: "stopped", true: "running"}[running], func(t *testing.T) {
			r := ollamatest.New(t)
			r.Fake.Lines = []ollamatest.Line{{"status": "success"}}
			pull(t, r, "deepseek-r1:14b")
			r.Runtime.SetState(map[bool]proc.State{false: proc.StateStopped, true: proc.StateRunning}[running])
			before := len(r.Runtime.Calls())

			require.NoError(t, r.Svc.Delete(context.Background(), "deepseek-r1:14b"))

			assert.Equal(t, []string{"deepseek-r1:14b"}, r.Fake.Deletes())
			assert.Equal(t, ollamapull.StateMissing, r.Svc.List(context.Background()).Models[2].State)
			want := []string{}
			if !running {
				want = []string{"prepare", "stop"}
			}
			assert.Equal(t, want, r.Runtime.Calls()[before:])
		})
	}
}

// A failure of Ollama's own delete is a failure in words, and the model stays in the list as it was.
func TestDeleteReportsWhatOllamaRefused(t *testing.T) {
	r := ollamatest.New(t)
	r.Fake.Lines = []ollamatest.Line{{"status": "success"}}
	pull(t, r, "gemma4:12b")
	r.Fake.DeleteStatus = 500

	err := r.Svc.Delete(context.Background(), "gemma4:12b")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Ollama がモデルを削除できませんでした")
}

// The models the running server holds that are not in the list are named, with their sizes, and only while it runs: a stopped one is never asked (and so never started).
func TestTheOtherModelsAreOnlyThoseOfARunningServerOutsideTheList(t *testing.T) {
	r := ollamatest.New(t)
	r.Fake.Tags = []ollamatest.Tag{{Name: "gemma4:12b", Size: 1}, {Name: "mistral:7b", Size: 4_100_000_000}, {Name: "hf.co/org/repo:Q4_K_M", Size: 5}}
	ctx := context.Background()

	assert.Empty(t, r.Svc.List(ctx).Others, "stopped")
	assert.NotContains(t, r.Fake.Requests(), "GET /api/tags")

	r.Runtime.SetState(proc.StateRunning)
	others := r.Svc.List(ctx).Others
	assert.Equal(t, []ollamapull.Other{{Name: "hf.co/org/repo:Q4_K_M", Size: 5}, {Name: "mistral:7b", Size: 4_100_000_000}}, others)
}

// The models that the LLM assignment may choose are the ones the running server holds; a stopped one gives the models of the list that are on disk.
func TestNamesAreTheModelsOfTheRunningServerElseThoseOnDisk(t *testing.T) {
	r := ollamatest.New(t)
	ctx := context.Background()
	assert.Empty(t, r.Svc.Names(ctx))
	r.Fake.Lines = []ollamatest.Line{{"status": "success"}}
	pull(t, r, "qwen3.8:27b")
	r.Runtime.SetState(proc.StateStopped)
	assert.Equal(t, []string{"qwen3.8:27b"}, r.Svc.Names(ctx))

	r.Fake.Tags = []ollamatest.Tag{{Name: "qwen3.8:27b"}, {Name: "mistral:7b"}}
	r.Runtime.SetState(proc.StateRunning)
	assert.Equal(t, []string{"mistral:7b", "qwen3.8:27b"}, r.Svc.Names(ctx))

	r.Runtime.Installed = false
	assert.Empty(t, r.Svc.Names(ctx), "no server, no model")
}

// A job that is no pull's is not read as one.
func TestJobStatusKnowsOnlyThePullsOfItsOwn(t *testing.T) {
	r := ollamatest.New(t)
	_, err := r.Svc.JobStatus(context.Background(), "no-such-job")
	assert.ErrorIs(t, err, modelmanager.ErrNotFound)
	assert.True(t, strings.HasPrefix(ollamapull.TargetPrefix, "ollama:"))
	assert.NotEmpty(t, catalog.Default().OllamaModels)
}
