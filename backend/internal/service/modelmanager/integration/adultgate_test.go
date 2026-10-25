package integration_test

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf/hftest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// waitUntilBlockedOnDestLock waits until a goroutine that runs fn (runHFInstall, runModelInstall) is blocked in lockDests, that
// is: it passed the checks that come before the lock and waits for another install of the same file. The code gives no other
// signal of that moment, and a test that closes the age gate before it would only test the early check.
func waitUntilBlockedOnDestLock(t *testing.T, fn string) {
	t.Helper()
	require.Eventually(t, func() bool {
		buf := make([]byte, 1<<20)
		for _, g := range strings.Split(string(buf[:runtime.Stack(buf, true)]), "\n\n") {
			if strings.Contains(g, "(*Service)."+fn) && strings.Contains(g, "(*Service).lockDests") && strings.Contains(g, "sync.(*Mutex)") {
				return true
			}
		}
		return false
	}, 30*time.Second, 5*time.Millisecond, "no install of the same file is waiting for the destination")
}

// waitForDownload waits until a download is in flight at the held CDN (the install that made it holds its destination).
func (r *rig) waitForDownload() {
	r.t.Helper()
	r.start()
	require.Eventually(r.t, func() bool { return r.cdnRequests() > 0 }, 30*time.Second, 5*time.Millisecond, "the download did not start")
}

// nsfwPony is a repository of adults that holds the file of hfRepo's pony.safetensors.
func nsfwPony() hftest.Repo {
	return hftest.Repo{ID: "o/nsfw", Commit: hfCommit, NFAA: true, Files: []hftest.File{{Path: "pony.safetensors", Body: sdxlBody}}}
}

// An import of an adult file that waits for the destination (another import of the same file is downloading) re-checks the
// age gate once it has the lock: the gate was closed while it waited, so it fails and records and writes nothing.
func TestTheGateClosedWhileAnImportWaitsForTheDestinationFailsIt(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	r.hf.Add(hfRepo())
	r.hf.Add(nsfwPony())
	r.confirmAdult()
	release := r.hf.HoldCDN(t)
	first, err := r.svc.InstallHFFile(ctx(), hfIn("pony.safetensors", "checkpoints", ""))
	require.NoError(t, err)
	r.waitForDownload()
	in := hfIn("pony.safetensors", "checkpoints", "")
	in.Repo = "o/nsfw"
	second, err := r.svc.InstallHFFile(ctx(), in)
	require.NoError(t, err, "the destination is empty when the request is made, and the gate is open")
	waitUntilBlockedOnDestLock(t, "runHFInstall")
	require.NoError(t, r.svc.SetAdultContent(false, nil))
	release()

	a, b := r.wait(first.JobID), r.wait(second.JobID)
	assert.Equal(t, domain.JobSucceeded, a.State, a.Error)
	require.Equal(t, domain.JobFailed, b.State, "the file is the same, so without the re-check it would be recorded as adult")
	assert.Contains(t, b.Error, "成人向け")
	assert.Equal(t, 1, r.cdnRequests(), "only the first download")
	models := r.svc.UserModels()
	require.Len(t, models, 1, "the record is the first one's: not replaced by an adult one")
	assert.Equal(t, "o/m", models[0].Repo)
	assert.False(t, models[0].NFAA)
}

// The same for a catalog install: an adult model whose file is being imported waits for the destination, the gate is closed,
// and the job fails without downloading or recording anything.
func TestTheGateClosedWhileACatalogInstallWaitsForTheDestinationFailsIt(t *testing.T) {
	r := newRig(t, withAdultModel())
	r.installEngine()
	r.confirmAdult()
	r.hf.Add(hftest.Repo{ID: "o/m", Commit: hfCommit, Files: []hftest.File{{Path: "a.safetensors", Body: otherPony()}}})
	release := r.hf.HoldCDN(t)
	imp, err := r.svc.InstallHFFile(ctx(), hfIn("a.safetensors", "checkpoints", ""))
	require.NoError(t, err)
	r.waitForDownload()
	m := r.mustModel("m-d")
	cat, err := r.svc.InstallModel(ctx(), "m-d", &modelmanager.LicenseRef{ID: m.License.ID, Version: m.License.Version})
	require.NoError(t, err, "nothing is recorded yet when the request is made, and the gate is open")
	waitUntilBlockedOnDestLock(t, "runModelInstall")
	before := len(r.host.requests())
	require.NoError(t, r.svc.SetAdultContent(false, nil))
	release()

	i, c := r.wait(imp.JobID), r.wait(cat.JobID)
	assert.Equal(t, domain.JobSucceeded, i.State, i.Error)
	require.Equal(t, domain.JobFailed, c.State)
	assert.Contains(t, c.Error, "成人向け", "refused for the gate, not for the file that the import wrote")
	assert.Equal(t, before, len(r.host.requests()), "nothing was downloaded")
	assert.NoFileExists(t, r.path(loraD))
	assert.NotContains(t, readState(t, r), `"m-d"`, "no record of the adult model")
}

// sdxlPony is the file that a user model and two catalog models (one for adults) share.
var sdxlPony = file{"checkpoints", "pony.safetensors", sdxlBody}

// withSharedPony adds the catalog models m-e (for adults) and m-p whose only file is sdxlPony, served by the fake host.
func withSharedPony() option {
	return func(d *modelmanager.Deps, r *rig) {
		r.host.mu.Lock()
		r.host.files["/"+sdxlPony.folder+"/"+sdxlPony.name] = sdxlPony
		r.host.mu.Unlock()
		model := func(id string, rating catalog.ContentRating) catalog.Model {
			return catalog.Model{
				ID: id, Name: id, Kind: localcfg.MediaImage, Tier: catalog.TierAdvanced, ContentRating: rating, Description: "d", ConsentRequired: true,
				Files:     []catalog.File{{Name: sdxlPony.name, Folder: sdxlPony.folder, URL: r.host.URL + "/" + sdxlPony.folder + "/" + sdxlPony.name, SHA256: sdxlPony.sha(), Size: int64(len(sdxlPony.body))}},
				VRAM:      catalog.VRAM{MinGB: 8, Source: "test", Estimate: true},
				License:   catalog.License{ID: "unspecified", Version: "1", Name: "unspecified", URL: "https://example.com/u", Summary: "s"},
				Workflows: []catalog.Workflow{{Name: "wf-" + id, Template: "txt2img-sdxl"}},
			}
		}
		d.Catalog.Models = append(d.Catalog.Models, model("m-e", catalog.RatingAdult), model("m-p", ""))
	}
}

// The list of the user models names the catalog models that hold the same file, but never an adult one while the age gate is
// closed: the list must not name a model it hides.
func TestUserModelSharedWithLeavesOutAnAdultCatalogModelWhileTheGateIsClosed(t *testing.T) {
	r := newRig(t, withSharedPony())
	r.installEngine()
	r.hf.Add(hfRepo())
	r.confirmAdult()
	require.Equal(t, domain.JobSucceeded, r.installAccepting("m-e").State)
	require.Equal(t, domain.JobSucceeded, r.installAccepting("m-p").State)
	r.hfInstallOK("pony.safetensors")

	models := r.svc.UserModels()
	require.Len(t, models, 1)
	assert.Equal(t, []string{"m-e", "m-p"}, models[0].SharedWith, "the gate is open")

	require.NoError(t, r.svc.SetAdultContent(false, nil))
	models = r.svc.UserModels()
	require.Len(t, models, 1, "the file itself is not adult")
	assert.Equal(t, []string{"m-p"}, models[0].SharedWith, "the adult model is not named")
}

// The file of a catalog model names the user model that holds it as shared ("HF: <name>"), but not an adult one (NFAA) while
// the age gate is closed.
func TestCatalogSharedWithLeavesOutAnAdultUserModelWhileTheGateIsClosed(t *testing.T) {
	r := newRig(t, withSharedPony())
	r.installEngine()
	repo := hfRepo()
	repo.NFAA = true
	r.hf.Add(repo)
	r.confirmAdult()
	r.hfInstallOK("pony.safetensors")
	require.Equal(t, domain.JobSucceeded, r.installAccepting("m-p").State)
	assert.Equal(t, []string{"HF: pony.safetensors"}, r.mustModel("m-p").Files[0].SharedWith, "the gate is open")

	require.NoError(t, r.svc.SetAdultContent(false, nil))
	assert.Empty(t, r.mustModel("m-p").Files[0].SharedWith, "the adult file is not named")
}

// The same bytes installed again from a repository that is not tagged keep the record adult (and keep its workflows): the
// file does not become general by naming another repository.
func TestTheSameBytesFromAGeneralRepositoryKeepTheUserModelAdult(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	r.hf.Add(hfRepo())
	r.hf.Add(nsfwPony())
	r.confirmAdult()
	in := hfIn("pony.safetensors", "checkpoints", "")
	in.Repo = "o/nsfw"
	ref, err := r.svc.InstallHFFile(ctx(), in)
	require.NoError(t, err)
	require.Equal(t, domain.JobSucceeded, r.wait(ref.JobID).State)
	wfs := r.svc.UserModels()[0].ModelIDs
	require.NotEmpty(t, wfs)

	r.hfInstallOK("pony.safetensors") // o/m: the file is in place with the same SHA-256, so it is recorded again
	models := r.svc.UserModels()
	require.Len(t, models, 1)
	assert.Equal(t, "o/m", models[0].Repo, "the record names the repository that was installed last")
	assert.True(t, models[0].NFAA)
	assert.Equal(t, wfs, models[0].ModelIDs, "the workflows are kept")

	require.NoError(t, r.svc.SetAdultContent(false, nil))
	assert.Empty(t, r.svc.UserModels(), "still adult: hidden while the gate is closed")
	assert.True(t, r.svc.HidesModel(wfs[0]), "and so are its models")
}
