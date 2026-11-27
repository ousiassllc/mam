package workflowstest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/workflows"
	"github.com/ousiassllc/mam/backend/internal/worktrack"
)

// panicEngine is a bundled ComfyUI whose start panics: the work of a run then panics before the backend is called.
type panicEngine struct{}

func (panicEngine) StartEngine(context.Context) (proc.Status, error) { panic("engine boom") }
func (panicEngine) Engine() modelmanager.EngineView                  { return modelmanager.EngineView{} }

// A panic in the work of a run must not end the process, and must not leave the record running: the run fails like any other.
func TestRunThatPanicsEndsAsFailed(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	graph, spec := template(t, "txt2img-sdxl")
	w, err := r.svc.Create(ctx, workflows.CreateInput{Name: "Stills", Kind: localcfg.MediaImage, API: graph, Spec: &spec})
	require.NoError(t, err)
	r.svc = workflows.New(workflows.Deps{
		Repo: r.repo, Models: r.local, Adapter: r.adapter, Keys: credentialtest.New(), Engine: panicEngine{},
		StagingDir: t.TempDir(), RunsDir: r.runs,
	})

	rec, err := r.svc.StartRun(ctx, workflows.RunInput{WorkflowID: w.ID, Prompt: "a red bicycle"})
	require.NoError(t, err)

	got := wait(t, r, rec.ID)
	assert.Equal(t, workflows.RunFailed, got.Status)
	assert.NotEmpty(t, got.Error)
	works := r.svc.Works()
	require.Len(t, works, 1)
	assert.Equal(t, worktrack.Failed, works[0].State)
}
