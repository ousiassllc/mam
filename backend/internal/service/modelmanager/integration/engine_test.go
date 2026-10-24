package integration_test

import (
	"context"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

func TestEngineViewBeforeAnInstall(t *testing.T) {
	r := newRig(t)
	v := r.svc.Engine()
	assert.True(t, v.Supported)
	assert.False(t, v.Installed)
	assert.Equal(t, "v1.0.0", v.PinnedVersion)
	assert.True(t, v.Writable)
	assert.Equal(t, r.layout.Root, v.Dir)
	assert.Equal(t, proc.StateStopped, v.Process.State)
	assert.False(t, v.AutoStart, "lazy start by default")
	require.Len(t, v.Variants, 1)
	assert.Positive(t, v.Variants[0].NeedBytes)
	assert.Equal(t, modelmanager.BundledEndpoint, v.Endpoint)
}

func TestInstallEngineDownloadsExtractsAndRegistersTheManagedEndpoint(t *testing.T) {
	r := newRig(t)
	var phases []string
	cancel := r.svc.Watch(func(e modelmanager.Event) {
		if e.Job != nil && (len(phases) == 0 || phases[len(phases)-1] != e.Job.Update.Phase) {
			phases = append(phases, e.Job.Update.Phase)
		}
	})
	defer cancel()
	ref, err := r.svc.InstallEngine(ctx(), modelmanager.EngineInstallInput{Variant: catalog.VariantNVIDIA})
	require.NoError(t, err)
	st := r.wait(ref.JobID)
	require.Equal(t, domain.JobSucceeded, st.State, st.Error)
	assert.Equal(t, "engine", st.Target)
	assert.Equal(t, 100, st.Progress.Percent)

	v := r.svc.Engine()
	assert.True(t, v.Installed)
	assert.Equal(t, "nvidia", v.Variant)
	assert.Empty(t, v.InstallJob, "the install is over")
	assert.Equal(t, []string{"download", "extract", "register", "done"}, phases)

	status, err := r.svc.Status(ctx())
	require.NoError(t, err)
	require.Len(t, status.Endpoints, 1)
	assert.Equal(t, modelmanager.BundledEndpoint, status.Endpoints[0].Name)
	assert.True(t, status.Endpoints[0].Bundled)
	assert.Contains(t, status.Endpoints[0].URL, "http://127.0.0.1:")
}

func TestInstallEngineRefusesBeforeDownloadingAnything(t *testing.T) {
	t.Run("not Windows", func(t *testing.T) {
		r := newRig(t, func(d *modelmanager.Deps, _ *rig) { d.Platform = "linux" })
		_, err := r.svc.InstallEngine(ctx(), modelmanager.EngineInstallInput{Variant: catalog.VariantNVIDIA})
		assert.ErrorIs(t, err, modelmanager.ErrUnsupported)
		assert.False(t, r.svc.Engine().Supported)
		_, err = r.svc.StartEngine(ctx())
		assert.ErrorIs(t, err, modelmanager.ErrUnsupported)
		assert.ErrorIs(t, r.svc.Prepare(ctx(), modelmanager.BundledEndpoint), modelmanager.ErrUnsupported)
	})
	t.Run("no supported GPU and no explicit CPU", func(t *testing.T) {
		r := newRig(t, func(d *modelmanager.Deps, _ *rig) {
			d.GPU = func(context.Context) gpu.Report {
				return gpu.Report{Recommendation: gpu.Recommendation{Guidance: "外部の URL を使ってください", CPUAvailable: true}}
			}
		})
		_, err := r.svc.InstallEngine(ctx(), modelmanager.EngineInstallInput{})
		require.ErrorIs(t, err, modelmanager.ErrNoGPU)
		assert.Contains(t, err.Error(), "外部の URL")
		assert.Empty(t, r.host.requests(), "nothing was downloaded")

		// The CPU run is offered only on an explicit choice: the NVIDIA package, started with --cpu.
		ref, err := r.svc.InstallEngine(ctx(), modelmanager.EngineInstallInput{CPU: true})
		require.NoError(t, err)
		assert.NotEmpty(t, ref.JobID)
	})
	t.Run("an unknown build", func(t *testing.T) {
		r := newRig(t)
		_, err := r.svc.InstallEngine(ctx(), modelmanager.EngineInstallInput{Variant: "tpu"})
		assert.Error(t, err)
	})
	t.Run("a directory that cannot be written", func(t *testing.T) {
		r := newRig(t)
		require.NoError(t, os.MkdirAll(r.layout.Root[:len(r.layout.Root)-len("comfyui")], 0o750))
		require.NoError(t, os.WriteFile(r.layout.Root, []byte("a file in the way"), 0o600))
		_, err := r.svc.InstallEngine(ctx(), modelmanager.EngineInstallInput{Variant: catalog.VariantNVIDIA})
		assert.ErrorIs(t, err, store.ErrNotWritable, "the screen offers the external URL")
		v := r.svc.Engine()
		assert.False(t, v.Writable)
		assert.NotEmpty(t, v.WritableError)
	})
	t.Run("not enough disk", func(t *testing.T) {
		r := newRig(t)
		r.free = func(string) (uint64, error) { return 1000, nil }
		_, err := r.svc.InstallEngine(ctx(), modelmanager.EngineInstallInput{Variant: catalog.VariantNVIDIA})
		var ns *fetch.NoSpaceError
		require.ErrorAs(t, err, &ns)
		assert.Empty(t, r.host.requests())
	})
}

func TestInstallEngineChoosesTheBuildForTheGPUWhenNoneIsGiven(t *testing.T) {
	r := newRig(t)
	ref, err := r.svc.InstallEngine(ctx(), modelmanager.EngineInstallInput{})
	require.NoError(t, err)
	st := r.wait(ref.JobID)
	require.Equal(t, domain.JobSucceeded, st.State, st.Error)
	assert.Equal(t, "nvidia", r.svc.Engine().Variant)
}

func TestTwoInstallsOfTheEngineAreOneJob(t *testing.T) {
	r := newRig(t)
	a, err := r.svc.InstallEngine(ctx(), modelmanager.EngineInstallInput{Variant: catalog.VariantNVIDIA})
	require.NoError(t, err)
	b, err := r.svc.InstallEngine(ctx(), modelmanager.EngineInstallInput{Variant: catalog.VariantNVIDIA})
	require.NoError(t, err)
	assert.Equal(t, a.JobID, b.JobID)
	assert.True(t, b.Existing)
	assert.Equal(t, a.JobID, r.svc.Engine().InstallJob)
}

func TestStartAndStopTheEngineKeepTheEndpointOnItsPort(t *testing.T) {
	r := newRig(t)
	_, err := r.svc.StartEngine(ctx())
	require.ErrorIs(t, err, modelmanager.ErrEngineNotInstalled)
	r.installEngine()

	var mu sync.Mutex
	var states []proc.State
	defer r.svc.Watch(func(e modelmanager.Event) {
		if e.Engine != nil {
			mu.Lock()
			states = append(states, e.Engine.State)
			mu.Unlock()
		}
	})()
	st, err := r.svc.StartEngine(ctx())
	require.NoError(t, err)
	assert.Equal(t, proc.StateRunning, st.State)
	status, _ := r.svc.Status(ctx())
	assert.Equal(t, st.URL, status.Endpoints[0].URL, "the managed endpoint points at the engine")

	// A call of a bundled model finds it running.
	require.NoError(t, r.svc.Prepare(ctx(), modelmanager.BundledEndpoint))
	require.NoError(t, r.svc.Prepare(ctx(), "somewhere-else"), "other endpoints are not its business")

	require.NoError(t, r.svc.StopEngine())
	assert.Equal(t, proc.StateStopped, r.svc.Engine().Process.State)
	mu.Lock()
	assert.Equal(t, []proc.State{proc.StateStarting, proc.StateRunning, proc.StateStopped}, states)
	mu.Unlock()

	// The same port is used again.
	again, err := r.svc.StartEngine(ctx())
	require.NoError(t, err)
	assert.Equal(t, st.Port, again.Port)
}

// The lazy start: the first call of a bundled model starts the engine. When the engine could not take the port the
// endpoint knows, the setting is updated and that call gets ErrEndpointMoved (the next one works).
func TestPrepareStartsTheEngineAndNoticesAMovedPort(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	require.NoError(t, r.svc.Prepare(ctx(), modelmanager.BundledEndpoint), "the endpoint already points at the remembered port")
	assert.Equal(t, proc.StateRunning, r.svc.Engine().Process.State)
	require.NoError(t, r.svc.StopEngine())

	// Somebody else took the remembered port: the engine comes up elsewhere.
	st0, _ := r.svc.Status(ctx())
	port, err := strconv.Atoi(st0.Endpoints[0].URL[len("http://127.0.0.1:"):])
	require.NoError(t, err)
	hold := listenOn(t, port)
	defer func() { _ = hold.Close() }()
	err = r.svc.Prepare(ctx(), modelmanager.BundledEndpoint)
	require.ErrorIs(t, err, modelmanager.ErrEndpointMoved)
	st1, _ := r.svc.Status(ctx())
	assert.NotEqual(t, st0.Endpoints[0].URL, st1.Endpoints[0].URL, "the setting follows the engine")
	assert.NoError(t, r.svc.Prepare(ctx(), modelmanager.BundledEndpoint))
}

func TestAutoStartStartsTheEngineWithMam(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	r.svc.OnStartup(ctx())
	assert.Equal(t, proc.StateStopped, r.svc.Engine().Process.State, "lazy by default")

	require.NoError(t, r.svc.SetAutoStart(true))
	assert.True(t, r.svc.Engine().AutoStart)
	r.svc.OnStartup(ctx())
	require.Eventually(t, func() bool { return r.svc.Engine().Process.State == proc.StateRunning }, 20*time.Second, 20*time.Millisecond)
	r.svc.Close()
	assert.Equal(t, proc.StateStopped, r.svc.Engine().Process.State)
}

func TestAFailedStartIsStatusWithTheReason(t *testing.T) {
	r := newRig(t, func(d *modelmanager.Deps, _ *rig) {
		d.Command = func(int) (proc.Spec, error) { return proc.Spec{Command: "/nonexistent/python.exe"}, nil }
	})
	r.installEngine()
	var failed []proc.Status
	var mu sync.Mutex
	defer r.svc.Watch(func(e modelmanager.Event) {
		if e.Engine != nil && e.Engine.State == proc.StateFailed {
			mu.Lock()
			failed = append(failed, *e.Engine)
			mu.Unlock()
		}
	})()
	_, err := r.svc.StartEngine(ctx())
	require.Error(t, err)
	v := r.svc.Engine()
	assert.Equal(t, proc.StateFailed, v.Process.State)
	assert.Contains(t, v.Process.Error, "起動できません")
	mu.Lock()
	assert.Len(t, failed, 1, "the chat and the screen hear about it")
	mu.Unlock()
}

func TestRemoveEngineKeepsTheModels(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	require.Equal(t, domain.JobSucceeded, r.installModel("m-a").State)
	_, err := r.svc.StartEngine(ctx())
	require.NoError(t, err)
	require.NoError(t, r.svc.RemoveEngine(ctx()))
	v := r.svc.Engine()
	assert.False(t, v.Installed)
	assert.Equal(t, proc.StateStopped, v.Process.State)
	assert.True(t, r.exists(ckptA), "the models are not the engine")
	_, err = r.svc.InstallModel(ctx(), "m-b", nil)
	assert.ErrorIs(t, err, modelmanager.ErrEngineNotInstalled)
}
