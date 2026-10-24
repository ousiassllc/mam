package workflowstest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/engine"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/workflows"
)

// engineStub is the bundled ComfyUI as the model manager reports it.
type engineStub struct {
	view    modelmanager.EngineView
	started int
}

func (e *engineStub) Engine() modelmanager.EngineView { return e.view }
func (e *engineStub) StartEngine(context.Context) (proc.Status, error) {
	e.started++
	return e.view.Process, nil
}

func withEngine(r *rig, view modelmanager.EngineView) (*workflows.Service, *engineStub) {
	stub := &engineStub{view: view}
	svc := workflows.New(workflows.Deps{Repo: r.repo, Models: r.local, Adapter: r.adapter, Engine: stub, RunsDir: r.runs})
	return svc, stub
}

func engineView(state proc.State, url string) modelmanager.EngineView {
	return modelmanager.EngineView{
		Supported: true, Status: engine.Status{Installed: true}, Process: proc.Status{State: state, URL: url},
	}
}

func TestNodeTargetOfTheBundledEngineFollowsItsState(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	cases := map[string]struct {
		view  modelmanager.EngineView
		state workflows.NodeState
	}{
		"running":       {engineView(proc.StateRunning, "http://127.0.0.1:8188"), workflows.NodeReady},
		"stopped":       {engineView(proc.StateStopped, ""), workflows.NodeStopped},
		"starting":      {engineView(proc.StateStarting, ""), workflows.NodeStarting},
		"failed":        {engineView(proc.StateFailed, ""), workflows.NodeFailed},
		"not installed": {modelmanager.EngineView{Supported: true}, workflows.NodeNotInstalled},
		"unsupported":   {modelmanager.EngineView{}, workflows.NodeUnsupported},
	}
	for name, c := range cases {
		svc, _ := withEngine(r, c.view)
		got := svc.NodeTarget(ctx, "127.0.0.1:8787", "")
		assert.Equal(t, c.state, got.State, name)
		assert.True(t, got.Bundled, name)
		if c.state != workflows.NodeReady {
			assert.NotEmpty(t, got.Reason, name)
			assert.Empty(t, got.FrameURL, name+": nothing to frame")
		}
	}
}

// The iframe uses the same literal host as the page: ComfyUI refuses what the browser calls cross-site, and
// 127.0.0.1 and localhost are different sites. Only a loopback request host is mirrored.
func TestNodeTargetUsesTheLiteralHostOfTheRequest(t *testing.T) {
	r := newRig(t)
	svc, _ := withEngine(r, engineView(proc.StateRunning, "http://127.0.0.1:8188"))
	for host, origin := range map[string]string{
		"127.0.0.1:8787": "http://127.0.0.1:8188",
		"localhost:8787": "http://localhost:8188",
		"127.0.0.2:9000": "http://127.0.0.2:8188",
		"[::1]:8787":     "http://127.0.0.1:8188",
		"evil.example":   "http://127.0.0.1:8188",
	} {
		got := svc.NodeTarget(context.Background(), host, "")
		assert.Equal(t, workflows.NodeReady, got.State, host)
		assert.Equal(t, origin, got.Origin, host)
		assert.Equal(t, origin+"/", got.FrameURL, host)
		assert.Equal(t, "http://"+host, got.MamOrigin, host)
	}
}

func TestNodeTargetOfAnExternalComfyUI(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	require.NoError(t, r.local.CreateEndpoint(ctx, localmodels.EndpointInput{Name: "tailnet", Kind: localcfg.KindComfyUI, BaseURL: r.comfy.URL()}))

	got := r.svc.NodeTarget(ctx, "127.0.0.1:8787", "tailnet")
	assert.Equal(t, workflows.NodeReady, got.State)
	assert.False(t, got.Bundled)
	assert.Equal(t, r.comfy.URL(), got.Origin, "exactly the configured endpoint's origin")
	assert.Equal(t, r.comfy.URL()+"/", got.OpenURL)

	// A ComfyUI on this machine is framed with the literal host the page was opened with, so it is the same site.
	r.comfy.RejectCrossSite = true
	got = r.svc.NodeTarget(ctx, "localhost:8787", "tailnet")
	assert.Equal(t, workflows.NodeReady, got.State)
	assert.Equal(t, "http://localhost:"+r.comfy.URL()[len("http://127.0.0.1:"):], got.Origin)

	// ComfyUI's own origin check refuses a page of another site (here: a page opened by a name that is not this
	// machine's loopback): the screen says so, with the way out.
	got = r.svc.NodeTarget(ctx, "mam.example:8787", "tailnet")
	assert.Equal(t, workflows.NodeRejected, got.State)
	assert.Contains(t, got.Reason, "--enable-cors-header http://mam.example:8787")
	assert.Equal(t, r.comfy.URL()+"/", got.OpenURL, "the link that opens it in the browser stays")

	r.comfy.Down = true
	got = r.svc.NodeTarget(ctx, "127.0.0.1:8787", "tailnet")
	assert.Equal(t, workflows.NodeUnreachable, got.State)
	assert.NotEmpty(t, got.Reason)

	assert.Equal(t, workflows.NodeUnreachable, r.svc.NodeTarget(ctx, "127.0.0.1:8787", "nowhere").State, "an unregistered endpoint is never framed")
	assert.Empty(t, r.svc.NodeTarget(ctx, "127.0.0.1:8787", "nowhere").Origin)
}

func TestStartStartsTheBundledEngineOnly(t *testing.T) {
	r := newRig(t)
	svc, stub := withEngine(r, engineView(proc.StateStopped, ""))
	require.NoError(t, svc.Start(context.Background()))
	assert.Equal(t, 1, stub.started)
	var invalid *workflows.InvalidError
	require.ErrorAs(t, r.svc.Start(context.Background()), &invalid, "no engine in this app")
}
