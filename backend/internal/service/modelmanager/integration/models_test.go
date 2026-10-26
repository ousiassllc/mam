package integration_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

func TestCatalogListsTheModelsWithStateLicenseAndSizes(t *testing.T) {
	r := newRig(t)
	views := r.svc.Catalog()
	require.Len(t, views, 3)
	a := views[0]
	assert.Equal(t, "m-a", a.ID)
	assert.Equal(t, modelmanager.ModelNotInstalled, a.State)
	assert.Equal(t, int64(len(ckptA.body)), a.TotalSize)
	assert.Equal(t, a.TotalSize, a.NeedBytes)
	assert.Equal(t, 8, a.VRAM.MinGB)
	assert.True(t, a.VRAM.Estimate)
	assert.True(t, a.License.CommercialUse)
	assert.False(t, a.ConsentRequired)
	assert.Equal(t, []string{"comfyui:bundled-comfyui/wf-a", "comfyui:bundled-comfyui/wf-a-i2i"}, a.ModelIDs)
	assert.Contains(t, a.WorkflowCheck, "GPU 実機未検証")
	c, err := r.svc.Model("m-c")
	require.NoError(t, err)
	assert.True(t, c.ConsentRequired)
	assert.False(t, c.Accepted)
	_, err = r.svc.Model("nope")
	assert.ErrorIs(t, err, modelmanager.ErrNotFound)
}

func TestInstallModelNeedsTheEngineAndRefusesBeforeDownloading(t *testing.T) {
	r := newRig(t)
	_, err := r.svc.InstallModel(ctx(), "m-a", nil)
	require.ErrorIs(t, err, modelmanager.ErrEngineNotInstalled)
	_, err = r.svc.InstallModel(ctx(), "nope", nil)
	require.ErrorIs(t, err, modelmanager.ErrNotFound)
	r.installEngine()
	before := len(r.host.requests())

	r.free = func(string) (uint64, error) { return uint64(len(ckptA.body)) - 1, nil }
	_, err = r.svc.InstallModel(ctx(), "m-a", nil)
	var ns *fetch.NoSpaceError
	require.ErrorAs(t, err, &ns, "no job is started when the disk is too small")
	assert.Equal(t, uint64(len(ckptA.body)), ns.Need)
	assert.Equal(t, before, len(r.host.requests()))
	assert.Equal(t, modelmanager.ModelNotInstalled, r.mustModel("m-a").State)
}

func (r *rig) mustModel(id string) modelmanager.ModelView {
	r.t.Helper()
	m, err := r.svc.Model(id)
	require.NoError(r.t, err)
	return m
}

func TestInstalledModelBecomesAModelOfTheManagedEndpoint(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	ref, err := r.svc.InstallModel(ctx(), "m-a", nil)
	require.NoError(t, err)
	assert.Equal(t, ref.JobID, r.mustModel("m-a").InstallJob)
	assert.Equal(t, modelmanager.ModelInstalling, r.mustModel("m-a").State)
	again, err := r.svc.InstallModel(ctx(), "m-a", nil)
	require.NoError(t, err)
	assert.Equal(t, ref.JobID, again.JobID, "one job for one model")
	assert.True(t, again.Existing)

	st := r.wait(ref.JobID)
	require.Equal(t, domain.JobSucceeded, st.State, st.Error)
	assert.Equal(t, "model:m-a", st.Target)
	assert.Equal(t, 100, st.Progress.Percent)

	got, err := os.ReadFile(r.path(ckptA))
	require.NoError(t, err)
	assert.Equal(t, ckptA.body, got)
	assert.Equal(t, modelmanager.ModelInstalled, r.mustModel("m-a").State)
	assert.Equal(t, []string{"comfyui:bundled-comfyui/wf-a", "comfyui:bundled-comfyui/wf-a-i2i"}, r.bundledModels())
	assert.Contains(t, domain.ImageModels(), domain.ModelName("comfyui:bundled-comfyui/wf-a"), "selectable where a #831 model is")

	_, err = r.svc.InstallModel(ctx(), "m-a", nil)
	assert.ErrorIs(t, err, modelmanager.ErrAlreadyInstalled)
}

// The progress runs by bytes over every file of the model, with the file the job is on in the message.
func TestInstallProgressCountsBytesAcrossFiles(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	var msgs []string
	var last int
	cancel := r.svc.Watch(func(e modelmanager.Event) {
		if e.Job != nil && strings.HasPrefix(e.Job.Target, "model:") {
			assert.GreaterOrEqual(t, e.Job.Update.Percent, last, "never goes back")
			last = e.Job.Update.Percent
			if len(msgs) == 0 || msgs[len(msgs)-1] != e.Job.Update.Message {
				msgs = append(msgs, e.Job.Update.Message)
			}
		}
	})
	defer cancel()
	st := r.installModel("m-b")
	require.Equal(t, domain.JobSucceeded, st.State, st.Error)
	assert.Contains(t, msgs[0], "b.safetensors（1/2）")
	assert.Contains(t, strings.Join(msgs, "|"), "shared.safetensors（2/2）")
	assert.Equal(t, 100, last)
}

func TestInstallResumesAPartialDownloadAndKeepsVerifiedFiles(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	// A previous run got half of the first file.
	require.NoError(t, os.MkdirAll(filepath.Dir(r.path(unetB)), 0o750))
	half := len(unetB.body) / 2
	require.NoError(t, os.WriteFile(fetch.PartPath(r.path(unetB)), unetB.body[:half], 0o600))
	need := r.mustModel("m-b").NeedBytes
	assert.Equal(t, int64(len(unetB.body)-half+len(vaeBC.body)), need, "what is downloaded already is not needed again")

	st := r.installModel("m-b")
	require.Equal(t, domain.JobSucceeded, st.State, st.Error)
	found := false
	for _, q := range r.host.requests() {
		found = found || q == "/diffusion_models/b.safetensors bytes="+strconv.Itoa(half)+"-"
	}
	assert.True(t, found, "the rest was asked for with a Range request: %v", r.host.requests())
	got, _ := os.ReadFile(r.path(unetB))
	assert.Equal(t, unetB.body, got)
}

func TestAWrongChecksumFailsTheJobAndLeavesNothing(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	r.host.mu.Lock()
	r.host.corrupt["/checkpoints/a.safetensors"] = true
	r.host.mu.Unlock()
	st := r.installModel("m-a")
	require.Equal(t, domain.JobFailed, st.State)
	assert.Contains(t, st.Error, "SHA-256")
	assert.False(t, r.exists(ckptA))
	assert.NoFileExists(t, fetch.PartPath(r.path(ckptA)))
	assert.Empty(t, r.bundledModels(), "no model appears")
	assert.Equal(t, modelmanager.ModelNotInstalled, r.mustModel("m-a").State)
	assert.Empty(t, r.mustModel("m-a").InstallJob)

	// Fixed at the source: the install can be started again.
	r.host.mu.Lock()
	delete(r.host.corrupt, "/checkpoints/a.safetensors")
	r.host.mu.Unlock()
	assert.Equal(t, domain.JobSucceeded, r.installModel("m-a").State)
}

func TestInstallFailsOnAnUnreachableFile(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	r.host.mu.Lock()
	delete(r.host.files, "/checkpoints/a.safetensors")
	r.host.mu.Unlock()
	st := r.installModel("m-a")
	require.Equal(t, domain.JobFailed, st.State)
	assert.Contains(t, st.Error, "a.safetensors")
	assert.Contains(t, st.Error, "404")
}

// The API enforces the consent: a model whose license needs an acceptance is not installed without one, with
// the right license ID and version, and a new version of the license asks again.
func TestLicenseConsentIsEnforcedByTheAPI(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	before := len(r.host.requests())
	_, err := r.svc.InstallModel(ctx(), "m-c", nil)
	require.ErrorIs(t, err, modelmanager.ErrConsentRequired)
	_, err = r.svc.InstallModel(ctx(), "m-c", &modelmanager.LicenseRef{ID: "apache-2.0", Version: "1"})
	var invalid *modelmanager.InvalidError
	require.ErrorAs(t, err, &invalid, "a different license is not the acceptance")
	_, err = r.svc.InstallModel(ctx(), "m-c", &modelmanager.LicenseRef{ID: "non-commercial", Version: "0"})
	require.ErrorAs(t, err, &invalid, "another version of the license either")
	assert.Equal(t, before, len(r.host.requests()), "nothing was downloaded")
	assert.False(t, r.mustModel("m-c").Accepted)

	ref, err := r.svc.InstallModel(ctx(), "m-c", &modelmanager.LicenseRef{ID: "non-commercial", Version: "1"})
	require.NoError(t, err)
	st := r.wait(ref.JobID)
	require.Equal(t, domain.JobSucceeded, st.State, st.Error)
	m := r.mustModel("m-c")
	assert.True(t, m.Accepted)
	assert.NotNil(t, m.AcceptedAt)

	// The acceptance is recorded: delete and install again without asking.
	require.NoError(t, r.svc.DeleteModel(ctx(), "m-c"))
	ref, err = r.svc.InstallModel(ctx(), "m-c", nil)
	require.NoError(t, err)
	assert.Equal(t, domain.JobSucceeded, r.wait(ref.JobID).State)

	assert.ErrorIs(t, r.svc.AcceptLicense("nope", modelmanager.LicenseRef{}), modelmanager.ErrNotFound)
	require.NoError(t, r.svc.AcceptLicense("m-c", modelmanager.LicenseRef{ID: "non-commercial", Version: "1"}))
}

func TestDeleteKeepsSharedFilesWhileAnotherModelUsesThem(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	require.Equal(t, domain.JobSucceeded, r.installModel("m-b").State)
	require.NoError(t, r.svc.AcceptLicense("m-c", modelmanager.LicenseRef{ID: "non-commercial", Version: "1"}))
	require.Equal(t, domain.JobSucceeded, r.installModel("m-c").State)
	assert.Equal(t, []string{"m-c"}, r.mustModel("m-b").Files[1].SharedWith, "the VAE is shared")

	require.NoError(t, r.svc.DeleteModel(ctx(), "m-b"))
	assert.False(t, r.exists(unetB), "its own file goes")
	assert.True(t, r.exists(vaeBC), "the shared VAE stays: m-c uses it")
	assert.True(t, r.exists(clipC))
	assert.Equal(t, []string{"comfyui:bundled-comfyui/wf-c"}, r.bundledModels(), "its workflows are gone from the endpoint")
	assert.Equal(t, modelmanager.ModelNotInstalled, r.mustModel("m-b").State)
	assert.ErrorIs(t, r.svc.DeleteModel(ctx(), "m-b"), modelmanager.ErrNotInstalled)

	require.NoError(t, r.svc.DeleteModel(ctx(), "m-c"))
	assert.False(t, r.exists(vaeBC), "the last user of the VAE is gone: the file goes")
	assert.False(t, r.exists(clipC))
	assert.Empty(t, r.bundledModels())

	// Installing again works from nothing.
	assert.Equal(t, domain.JobSucceeded, r.installModel("m-b").State)
	assert.True(t, r.exists(vaeBC))
}

func TestDeleteRefusesAModelThatIsChosenSomewhere(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	require.Equal(t, domain.JobSucceeded, r.installModel("m-a").State)
	view, err := r.sets.View()
	require.NoError(t, err)
	in := modelsettings.Input{Images: view.Images, Policies: view.Policies}
	in.Images[modelsettings.PurposeEditReference] = modelsettings.ImageRoute{
		Primary: modelsettings.ImageTarget{Model: "comfyui:bundled-comfyui/wf-a-i2i"}, Fallback: modelsettings.ImageTarget{Model: domain.ModelNanoBanana},
	}
	require.NoError(t, r.sets.Save(in))

	err = r.svc.DeleteModel(ctx(), "m-a")
	var invalid *modelmanager.InvalidError
	require.ErrorAs(t, err, &invalid)
	assert.Contains(t, invalid.Message, "images.edit_reference.primary")
	assert.True(t, r.exists(ckptA), "nothing was deleted")
	assert.Len(t, r.bundledModels(), 2, "neither of the two models")
	assert.Equal(t, modelmanager.ModelInstalled, r.mustModel("m-a").State)
}

func TestAFileDeletedByHandMakesTheModelIncomplete(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	require.Equal(t, domain.JobSucceeded, r.installModel("m-b").State)
	require.NoError(t, os.Remove(r.path(vaeBC)))
	m := r.mustModel("m-b")
	assert.Equal(t, modelmanager.ModelIncomplete, m.State)
	assert.False(t, m.Files[1].Present)
	assert.NoError(t, r.svc.DeleteModel(ctx(), "m-b"), "it can be cleaned up")
}

func TestInstallIsRefusedWhenTheModelFileIsPinned(t *testing.T) {
	r := newPinnedRig(t)
	r.installEngine() // the engine is installed; only the endpoint of the managed models cannot be written
	_, err := r.svc.InstallModel(ctx(), "m-a", nil)
	assert.ErrorIs(t, err, modelmanager.ErrPinned)
	status, err := r.svc.Status(ctx())
	require.NoError(t, err)
	assert.True(t, status.Pinned)
}

func TestHuggingFaceTokenIsOptionalAndNeverReturned(t *testing.T) {
	r := newRig(t)
	ok, _, err := r.svc.HuggingFaceTokenStatus()
	require.NoError(t, err)
	assert.False(t, ok)
	var invalid *modelmanager.InvalidError
	require.ErrorAs(t, r.svc.SetHuggingFaceToken("  "), &invalid)
	require.ErrorAs(t, r.svc.SetHuggingFaceToken("has space inside"), &invalid)
	require.NoError(t, r.svc.SetHuggingFaceToken("hf_abcdefghijklmnop1234"))
	var last string
	ok, last, err = r.svc.HuggingFaceTokenStatus()
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "1234", last)
	got, err := r.keys.Get(modelmanager.HuggingFaceProvider)
	require.NoError(t, err)
	assert.Equal(t, "hf_abcdefghijklmnop1234", got)

	// The test server is not huggingface.co: it never sees the token.
	r.installEngine()
	require.Equal(t, domain.JobSucceeded, r.installModel("m-a").State)
	for _, a := range r.host.auth {
		assert.Empty(t, a)
	}
	require.NoError(t, r.svc.ClearHuggingFaceToken())
	require.NoError(t, r.svc.ClearHuggingFaceToken(), "clearing a missing token is not an error")
	ok, _, _ = r.svc.HuggingFaceTokenStatus()
	assert.False(t, ok)
}
