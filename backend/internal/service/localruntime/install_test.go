package localruntime_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

func ctx() context.Context { return context.Background() }

func TestStatusBeforeAnInstallDescribesTheRuntimeAndTheMachine(t *testing.T) {
	r := newRig(t)
	v, err := r.svc.Status(ctx(), "gpt-sovits")
	require.NoError(t, err)
	assert.Equal(t, "gpt-sovits", v.ID)
	assert.Equal(t, catalog.PurposeTTS, v.Purpose)
	assert.True(t, v.Support.Supported)
	assert.Equal(t, catalog.VariantNVIDIA, v.Support.Variant)
	assert.False(t, v.Installed)
	assert.Equal(t, "1", v.PinnedVersion)
	assert.True(t, v.Writable)
	assert.Equal(t, r.layout.Root, v.Dir)
	assert.Equal(t, proc.StateStopped, v.Process.State)
	assert.False(t, v.AutoStart, "lazy start by default")
	assert.Equal(t, "mit", v.License.ID)
	require.Len(t, v.Variants, 2)
	assert.Positive(t, v.Variants[0].NeedBytes)
	assert.Positive(t, v.FreeBytes)
	assert.Len(t, r.svc.List(ctx()), 1)

	_, err = r.svc.Status(ctx(), "nope")
	assert.ErrorIs(t, err, localruntime.ErrNotFound)
}

func TestInstallDownloadsExtractsAndReportsProgressAsAJobOfTheTargetRuntime(t *testing.T) {
	r := newRig(t)
	var phases []string
	cancel := r.svc.Watch(func(e localruntime.Event) {
		if e.Job != nil && (len(phases) == 0 || phases[len(phases)-1] != e.Job.Update.Phase) {
			phases = append(phases, e.Job.Update.Phase)
		}
	})
	defer cancel()
	ref, err := r.svc.Install(ctx(), "gpt-sovits")
	require.NoError(t, err)
	st := r.wait(ref.JobID)
	require.Equal(t, domain.JobSucceeded, st.State, st.Error)
	assert.Equal(t, domain.JobModelInstall, st.Kind, "the job kind of the model manager is reused: no new migration")
	assert.Equal(t, "runtime:gpt-sovits", st.Target)
	assert.Equal(t, 100, st.Progress.Percent)
	assert.Equal(t, []string{"download", "extract", "done"}, phases)

	v, err := r.svc.Status(ctx(), "gpt-sovits")
	require.NoError(t, err)
	assert.True(t, v.Installed)
	assert.Equal(t, "nvidia", v.Variant)
	assert.Empty(t, v.InstallJob, "the install is over")
	assert.FileExists(t, filepath.Join(r.layout.EnvDir(), filepath.FromSlash(entryRel)))
	assert.NoDirExists(t, r.layout.DownloadsDir())
}

func TestTheBuildFollowsTheGenerationOfTheGPU(t *testing.T) {
	r := newRig(t, withGPU(rtx("NVIDIA GeForce RTX 5080", 16, 12.0)))
	r.install()
	v, err := r.svc.Status(ctx(), "gpt-sovits")
	require.NoError(t, err)
	assert.Equal(t, "nvidia50", v.Variant)
}

func TestInstallTwiceGivesTheSameJobAndRemoveDuringAnInstallIsBusy(t *testing.T) {
	r := newRig(t)
	release := r.host.Hold()
	t.Cleanup(release)
	first, err := r.svc.Install(ctx(), "gpt-sovits")
	require.NoError(t, err)
	r.start()
	r.host.Reached(t)

	again, err := r.svc.Install(ctx(), "gpt-sovits")
	require.NoError(t, err)
	assert.Equal(t, first.JobID, again.JobID)
	assert.True(t, again.Existing)
	v, _ := r.svc.Status(ctx(), "gpt-sovits")
	assert.Equal(t, first.JobID, v.InstallJob)

	assert.ErrorIs(t, r.svc.Remove(ctx(), "gpt-sovits"), localruntime.ErrBusy)
	_, err = r.svc.Start(ctx(), "gpt-sovits")
	assert.ErrorIs(t, err, localruntime.ErrNotInstalled)

	release()
	require.Equal(t, domain.JobSucceeded, r.wait(first.JobID).State)
	require.NoError(t, r.svc.Remove(ctx(), "gpt-sovits"))
	v, _ = r.svc.Status(ctx(), "gpt-sovits")
	assert.False(t, v.Installed)
	assert.NoDirExists(t, r.layout.EnvDir())
}

// The job of a runtime is told from every other model_install job by its target: the others go on to the model manager's handler.
func TestRegisterLeavesTheOtherInstallJobsToTheModelManager(t *testing.T) {
	r := newRig(t)
	job, err := r.queue.Enqueue(ctx(), domain.JobModelInstall, map[string]string{"target": "engine"})
	require.NoError(t, err)
	r.start()
	got := <-r.others
	assert.Equal(t, job.ID, got.ID)
	_, err = r.svc.JobStatus(ctx(), job.ID)
	assert.ErrorIs(t, err, localruntime.ErrNotFound, "the install of the engine is not a runtime's job")
	_, err = r.svc.JobStatus(ctx(), "nope")
	assert.ErrorIs(t, err, localruntime.ErrNotFound)
}

func TestInterruptedDownloadResumesWithARangeRequest(t *testing.T) {
	r := newRig(t)
	r.host.cut = true
	ref, err := r.svc.Install(ctx(), "gpt-sovits")
	require.NoError(t, err)
	st := r.wait(ref.JobID)
	require.Equal(t, domain.JobFailed, st.State, "the connection dropped in the middle of the archive")
	v, _ := r.svc.Status(ctx(), "gpt-sovits")
	assert.False(t, v.Installed)
	part, err := os.Stat(filepath.Join(r.layout.DownloadsDir(), "pkg.7z.part"))
	require.NoError(t, err, "the part file is kept for the resume")

	r.install()
	ranges := r.host.Ranges()
	require.Len(t, ranges, 2)
	assert.Empty(t, ranges[0])
	assert.Equal(t, "bytes="+strconv.FormatInt(part.Size(), 10)+"-", ranges[1], "continues where the first run stopped")
}

func TestCancelInstallStopsADownloadAndTheNextInstallResumes(t *testing.T) {
	r := newRig(t)
	release := r.host.Hold()
	t.Cleanup(release)
	ref, err := r.svc.Install(ctx(), "gpt-sovits")
	require.NoError(t, err)
	r.start()
	r.host.Reached(t)
	require.Eventually(t, func() bool { return r.svc.CancelInstall(ref.JobID) == nil }, 20e9, 10e6)

	st := r.wait(ref.JobID)
	assert.Equal(t, domain.JobFailed, st.State)
	assert.Contains(t, st.Error, "導入を中断しました")
	assert.Error(t, r.svc.CancelInstall(ref.JobID), "nothing runs under that job any more")
	assert.ErrorIs(t, r.svc.CancelInstall("nope"), localruntime.ErrNotFound)
	release()
	r.install()
}

func TestInstallFailuresAreToldInJapaneseAndLeaveNothingBehind(t *testing.T) {
	for name, tc := range map[string]struct {
		edit func(h *host, c *catalog.Catalog)
		want string
		err  error
	}{
		"a hash that does not match": {func(_ *host, c *catalog.Catalog) {
			v := c.Runtimes[0].Variants[catalog.VariantNVIDIA]
			v.SHA256 = strings.Repeat("0", 64)
			c.Runtimes[0].Variants[catalog.VariantNVIDIA] = v
		}, "SHA-256 が一致しません", fetch.ErrChecksum},
		"a size that does not match": {func(_ *host, c *catalog.Catalog) {
			v := c.Runtimes[0].Variants[catalog.VariantNVIDIA]
			v.Size--
			c.Runtimes[0].Variants[catalog.VariantNVIDIA] = v
		}, "大きさが固定した値と違います", fetch.ErrSize},
		"a host that has moved the file": {func(h *host, c *catalog.Catalog) {
			v := c.Runtimes[0].Variants[catalog.VariantNVIDIA]
			v.URL = h.URL + "/gone"
			c.Runtimes[0].Variants[catalog.VariantNVIDIA] = v
		}, "取得元が応答しません（HTTP 404）", nil},
	} {
		t.Run(name, func(t *testing.T) {
			host := newHost(t)
			cat := testCatalog(host)
			tc.edit(host, cat)
			r := newRig(t, func(r *rig, d *localruntime.Deps) { d.Catalog = cat; r.host = host })
			ref, err := r.svc.Install(ctx(), "gpt-sovits")
			require.NoError(t, err)
			st := r.wait(ref.JobID)
			require.Equal(t, domain.JobFailed, st.State)
			assert.Contains(t, st.Error, tc.want)
			v, _ := r.svc.Status(ctx(), "gpt-sovits")
			assert.False(t, v.Installed)
			entries, _ := os.ReadDir(r.layout.DownloadsDir())
			assert.Empty(t, entries, "neither the archive nor its part file is left")
			assert.NoDirExists(t, r.layout.EnvDir())
		})
	}
}

func TestInstallRefusesAtOnceWithoutRoomOnTheDisk(t *testing.T) {
	r := newRig(t)
	r.free = uint64(len(r.host.body)) // room for the archive, not for its files
	_, err := r.svc.Install(ctx(), "gpt-sovits")
	var ns *fetch.NoSpaceError
	require.ErrorAs(t, err, &ns)
	assert.Empty(t, r.host.Ranges(), "nothing was requested")
}

func TestInstallRefusesAPlaceThatCannotBeWritten(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write to a read-only directory")
	}
	root := filepath.Join(t.TempDir(), "localai")
	require.NoError(t, os.Mkdir(root, 0o500)) // a portable mam.exe in a read-only place
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })
	r := newRig(t, func(_ *rig, d *localruntime.Deps) { d.Root = root })
	_, err := r.svc.Install(ctx(), "gpt-sovits")
	require.ErrorIs(t, err, store.ErrNotWritable)
	v, _ := r.svc.Status(ctx(), "gpt-sovits")
	assert.False(t, v.Writable)
	assert.NotEmpty(t, v.WritableError)
	assert.Empty(t, r.host.Ranges())
}
