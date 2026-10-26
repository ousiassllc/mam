package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf/hftest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

func TestDeletingAUserModelRemovesItsFileAndItsModels(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	r.hf.Add(hfRepo())
	r.hfInstallOK("pony.safetensors")
	r.hfInstallOK("style-lora.safetensors")
	require.Len(t, r.bundledModels(), 2)

	require.ErrorIs(t, r.svc.DeleteUserModel(ctx(), "models/checkpoints/none.safetensors"), modelmanager.ErrNotInstalled)
	require.NoError(t, r.svc.DeleteUserModel(ctx(), "models/checkpoints/pony.safetensors"))
	assert.NoFileExists(t, r.hfPath("checkpoints", "pony.safetensors"))
	assert.Empty(t, r.bundledModels())
	require.Len(t, r.svc.UserModels(), 1)
	assert.FileExists(t, r.hfPath("loras", "style-lora.safetensors"))

	require.NoError(t, r.svc.DeleteUserModel(ctx(), "models/loras/style-lora.safetensors"))
	assert.Empty(t, r.svc.UserModels())
	r.hfInstallOK("pony.safetensors") // and it can be installed again from nothing
	assert.Len(t, r.bundledModels(), 2)
}

func TestDeletingAUserModelThatIsChosenSomewhereIsRefused(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	r.hf.Add(hfRepo())
	r.hfInstallOK("pony.safetensors")
	i2i := r.svc.UserModels()[0].ModelIDs[1]
	view, err := r.sets.View()
	require.NoError(t, err)
	in := modelsettings.Input{Images: view.Images, Policies: view.Policies}
	in.Images[modelsettings.PurposeEditReference] = modelsettings.ImageRoute{
		Primary: modelsettings.ImageTarget{Model: domain.ModelName(i2i)}, Fallback: modelsettings.ImageTarget{Model: domain.ModelNanoBanana},
	}
	require.NoError(t, r.sets.Save(in))

	err = r.svc.DeleteUserModel(ctx(), "models/checkpoints/pony.safetensors")
	var invalid *modelmanager.InvalidError
	require.ErrorAs(t, err, &invalid)
	assert.Contains(t, invalid.Message, "images.edit_reference.primary")
	assert.FileExists(t, r.hfPath("checkpoints", "pony.safetensors"), "nothing was deleted")
	assert.Len(t, r.svc.UserModels(), 1)
}

// A file of a catalog model and a file the user added are the same file when the folder, the name and the SHA-256 agree: the
// file is kept while either of them remains, and the user's install does not download it again.
func TestAFileSharedWithACatalogModelStaysWhileEitherIsInstalled(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	require.Equal(t, domain.JobSucceeded, r.installModel("m-a").State)
	r.hf.Add(hftest.Repo{ID: "o/a", Commit: hfCommit, Files: []hftest.File{{Path: "a.safetensors", Body: ckptA.body}}})
	in := hfIn("a.safetensors", "checkpoints", "")
	in.Repo = "o/a"
	before := len(r.host.requests())
	ref, err := r.svc.InstallHFFile(ctx(), in)
	require.NoError(t, err, "the same file the catalog placed is not a conflict")
	require.Equal(t, domain.JobSucceeded, r.wait(ref.JobID).State)
	assert.Equal(t, before, len(r.host.requests()))
	assert.Zero(t, r.cdnRequests(), "it is not fetched again")
	assert.Equal(t, []string{"HF: a.safetensors"}, r.mustModel("m-a").Files[0].SharedWith)
	um := r.svc.UserModels()[0]
	assert.Equal(t, []string{"m-a"}, um.SharedWith)

	require.NoError(t, r.svc.DeleteModel(ctx(), "m-a"))
	assert.True(t, r.exists(ckptA), "the file the user added stays")
	assert.Empty(t, r.bundledModels())

	require.Equal(t, domain.JobSucceeded, r.installModel("m-a").State)
	require.NoError(t, r.svc.DeleteUserModel(ctx(), um.Path))
	assert.True(t, r.exists(ckptA), "the catalog model still uses it")
	require.NoError(t, r.svc.DeleteModel(ctx(), "m-a"))
	assert.False(t, r.exists(ckptA), "the last user is gone: the file goes")
}
