package integration_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf/hftest"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hfplan"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

const hfCommit = "0123456789abcdef0123456789abcdef01234567"

var (
	// the bodies the fake Hugging Face serves; the header of each tells what it is
	sdxlBody = hftest.Safetensors([]string{"model.diffusion_model.a", "conditioner.embedders.1.model.b", "first_stage_model.c"}, nil, 4000)
	loraBody = hftest.Safetensors([]string{"lora_unet_a.lora_down.weight", "lora_unet_a.alpha"}, nil, 500)
	vaeBody  = hftest.Safetensors([]string{"encoder.x", "decoder.y", "quant_conv.z"}, nil, 500)
)

// hfRepo is a repository of the fake Hugging Face with a checkpoint, a LoRA and a VAE and the files that cannot be installed.
func hfRepo() hftest.Repo {
	return hftest.Repo{ID: "o/m", Commit: hfCommit, License: "openrail++", Files: []hftest.File{
		{Path: "pony.safetensors", Body: sdxlBody},
		{Path: "style-lora.safetensors", Body: loraBody},
		{Path: "split_files/vae/ae.safetensors", Body: vaeBody},
		{Path: "m.Q4_K_M.gguf", Body: []byte("GGUF....")},
		{Path: "old.ckpt", Body: []byte("pickle..")},
		{Path: "unet/diffusion_pytorch_model.safetensors", Body: sdxlBody},
		{Path: "plain.safetensors", Body: sdxlBody, NoLFS: true},
		{Path: "bad.safetensors", Body: sdxlBody, Security: "unsafe"},
	}}
}

func (r *rig) hfInstall(in modelmanager.HFInstallInput) (modelmanager.JobRef, error) {
	r.t.Helper()
	in.Repo, in.Commit = "o/m", hfCommit
	return r.svc.InstallHFFile(ctx(), in)
}

// hfInstallOK installs a file with the terms accepted and waits for the job.
func (r *rig) hfInstallOK(path string) modelmanager.JobStatus {
	r.t.Helper()
	ref, err := r.hfInstall(modelmanager.HFInstallInput{Path: path, Accept: true, TermsVersion: hfplan.TermsVersion})
	require.NoError(r.t, err)
	st := r.wait(ref.JobID)
	require.Equal(r.t, domain.JobSucceeded, st.State, st.Error)
	return st
}

// cdnRequests counts the downloads from the CDN: a request without a Range, or one that continues a part file (the header
// reads that guess a folder ask for a closed range).
func (r *rig) cdnRequests() int {
	n := 0
	for _, q := range r.hf.Requests() {
		if strings.HasPrefix(q.Path, "/cdn/") && (q.Range == "" || strings.HasSuffix(q.Range, "-")) {
			n++
		}
	}
	return n
}

func (r *rig) hfPath(folder, name string) string {
	return r.layout.ModelPath("models/" + folder + "/" + name)
}

func TestHFRepoListsTheFilesOfEveryKindOfInput(t *testing.T) {
	r := newRig(t)
	r.hf.Add(hfRepo())
	for _, in := range []string{
		"o/m", "https://huggingface.co/o/m", "https://huggingface.co/o/m/tree/main",
		"https://huggingface.co/o/m/blob/" + hfCommit + "/pony.safetensors",
	} {
		v, err := r.svc.HFRepo(ctx(), in)
		require.NoError(t, err, in)
		assert.Equal(t, hfCommit, v.Repo.Commit, in)
		assert.Len(t, v.Files, 8, in)
	}
	v, err := r.svc.HFRepo(ctx(), "https://huggingface.co/o/m/blob/main/pony.safetensors")
	require.NoError(t, err)
	assert.Equal(t, "pony.safetensors", v.Selected)
	assert.Equal(t, hfplan.Terms, v.Terms)
	assert.False(t, v.Consented)

	var invalid *modelmanager.InvalidError
	for _, bad := range []string{"", "nope", "https://hf.co/o/m", "http://huggingface.co/o/m", "https://huggingface.co/o/m?x=1", "https://evil.example/o/m"} {
		_, err := r.svc.HFRepo(ctx(), bad)
		require.ErrorAs(t, err, &invalid, bad)
	}
	_, err = r.svc.HFRepo(ctx(), "o/none")
	require.ErrorAs(t, err, &invalid)
	assert.Contains(t, invalid.Message, "o/none")
}

func TestHFRepoFollowsARenameAndSearchListsHits(t *testing.T) {
	r := newRig(t)
	r.hf.Add(hfRepo())
	r.hf.Rename("o/old", "o/m")
	v, err := r.svc.HFRepo(ctx(), "o/old")
	require.NoError(t, err)
	assert.Equal(t, "o/m", v.Repo.ID)

	hits, err := r.svc.SearchHF(ctx(), "o/m", "", 20)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, "o/m", hits[0].Repo)
	var invalid *modelmanager.InvalidError
	_, err = r.svc.SearchHF(ctx(), "  ", "", 20)
	require.ErrorAs(t, err, &invalid)
	_, err = r.svc.SearchHF(ctx(), "x", "text-to-audio", 20)
	require.ErrorAs(t, err, &invalid)
}

func TestInstallAnSDXLCheckpointPlacesItRecordsItAndMakesTwoModels(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	r.hf.Add(hfRepo())

	st := r.hfInstallOK("pony.safetensors")
	assert.Equal(t, "hf:o/m@"+hfCommit+"/pony.safetensors", st.Target)
	assert.Equal(t, 100, st.Progress.Percent)
	data, err := os.ReadFile(r.hfPath("checkpoints", "pony.safetensors"))
	require.NoError(t, err)
	assert.Equal(t, sdxlBody, data)
	assert.NoFileExists(t, r.hfPath("checkpoints", "pony.safetensors")+".part")

	list := r.svc.UserModels()
	require.Len(t, list, 1)
	um := list[0]
	assert.Equal(t, "models/checkpoints/pony.safetensors", um.Path)
	assert.Equal(t, "o/m", um.Repo)
	assert.Equal(t, hfCommit, um.Commit)
	assert.Equal(t, "pony.safetensors", um.RepoPath)
	assert.Equal(t, "checkpoints", um.Folder)
	assert.Equal(t, hftest.SHA(sdxlBody), um.SHA256)
	assert.Equal(t, int64(len(sdxlBody)), um.Size)
	assert.Equal(t, "openrail++", um.License.ID)
	assert.Equal(t, hfplan.FamilySDXL, um.Family)
	assert.False(t, um.InstalledAt.IsZero())
	assert.True(t, um.Present)
	require.Len(t, um.ModelIDs, 2)
	assert.ElementsMatch(t, um.ModelIDs, r.bundledModels(), "text to image and reference to image, on the managed endpoint")
	assert.Contains(t, um.ModelIDs[0], "comfyui:bundled-comfyui/hf-pony-")
	assert.Contains(t, readState(t, r), hfplan.ConsentKey("o/m", hfCommit), "the terms were recorded for the repository and commit")

	v, err := r.svc.HFRepo(ctx(), "o/m")
	require.NoError(t, err)
	assert.True(t, v.Consented)
	for _, f := range v.Files {
		same := f.Path == "pony.safetensors" || f.Path == "unet/diffusion_pytorch_model.safetensors" || f.Path == "bad.safetensors" // the same bytes (the SHA-256)
		assert.Equal(t, same, f.Installed, f.Path)
	}
}

func TestOnlyACheckpointOfTheSDXLFamilyGetsModelsTheOthersArePlacedOnly(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	r.hf.Add(hfRepo())
	r.hfInstallOK("style-lora.safetensors")
	r.hfInstallOK("split_files/vae/ae.safetensors")
	assert.FileExists(t, r.hfPath("loras", "style-lora.safetensors"), "the header says LoRA")
	assert.FileExists(t, r.hfPath("vae", "ae.safetensors"), "the path says vae")
	assert.Empty(t, r.bundledModels(), "no workflow for a LoRA (it waits for the LoRA templates) or a VAE")
	for _, um := range r.svc.UserModels() {
		assert.Empty(t, um.ModelIDs)
	}
}

func TestTheTermsAreEnforcedByTheAPIAndRecordedPerRepositoryAndCommit(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	r.hf.Add(hfRepo())

	_, err := r.hfInstall(modelmanager.HFInstallInput{Path: "pony.safetensors"})
	require.ErrorIs(t, err, modelmanager.ErrConsentRequired)
	var invalid *modelmanager.InvalidError
	_, err = r.hfInstall(modelmanager.HFInstallInput{Path: "pony.safetensors", Accept: true, TermsVersion: "0"})
	require.ErrorAs(t, err, &invalid, "the wording of another version")
	assert.Zero(t, r.cdnRequests(), "nothing was downloaded")
	assert.NotContains(t, readState(t, r), "hf_consents")

	r.hfInstallOK("pony.safetensors")
	ref, err := r.hfInstall(modelmanager.HFInstallInput{Path: "style-lora.safetensors"})
	require.NoError(t, err, "a second file of the same repository and commit needs no new confirmation")
	require.Equal(t, domain.JobSucceeded, r.wait(ref.JobID).State)

	r.hf.Add(hftest.Repo{ID: "o/other", Commit: hfCommit, Files: []hftest.File{{Path: "x.safetensors", Body: loraBody}}})
	_, err = r.svc.InstallHFFile(ctx(), modelmanager.HFInstallInput{Repo: "o/other", Commit: hfCommit, Path: "x.safetensors", Folder: "loras"})
	require.ErrorIs(t, err, modelmanager.ErrConsentRequired, "another repository asks again")
}
