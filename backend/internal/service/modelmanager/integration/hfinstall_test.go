package integration_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf/hftest"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hfplan"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// hfIn is an install of a file of "o/m" with the terms accepted.
func hfIn(path, folder, name string) modelmanager.HFInstallInput {
	return modelmanager.HFInstallInput{Repo: "o/m", Commit: hfCommit, Path: path, Folder: folder, Name: name, Accept: true, TermsVersion: hfplan.TermsVersion}
}

func TestOnlySafetensorsAreInstalledAndNothingIsDownloadedForTheRest(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	r.hf.Add(hfRepo())
	for path, want := range map[string]string{
		"m.Q4_K_M.gguf": hfplan.ReasonGGUF,
		"old.ckpt":      hfplan.ReasonPickle,
		"unet/diffusion_pytorch_model.safetensors": hfplan.ReasonSplit,
		"plain.safetensors":                        hfplan.ReasonNoLFS,
		"bad.safetensors":                          hfplan.ReasonUnsafe,
	} {
		_, err := r.svc.InstallHFFile(ctx(), hfIn(path, "", ""))
		var invalid *modelmanager.InvalidError
		require.ErrorAs(t, err, &invalid, path)
		assert.Contains(t, invalid.Message, want, path)
	}
	assert.Zero(t, r.cdnRequests())
	assert.Empty(t, r.svc.UserModels())
	assert.NotContains(t, readState(t, r), "hf_consents", "a refused install records no confirmation")
}

func TestAWrongChecksumFailsTheJobAndLeavesNoFile(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	swapped := append([]byte(nil), sdxlBody...)
	swapped[len(swapped)-1] ^= 0xff
	repo := hfRepo()
	repo.Files[0].Served = swapped
	r.hf.Add(repo)

	ref, err := r.svc.InstallHFFile(ctx(), hfIn("pony.safetensors", "", ""))
	require.NoError(t, err)
	st := r.wait(ref.JobID)
	require.Equal(t, domain.JobFailed, st.State)
	assert.Contains(t, st.Error, "SHA-256")
	assert.NoFileExists(t, r.hfPath("checkpoints", "pony.safetensors"))
	assert.NoFileExists(t, r.hfPath("checkpoints", "pony.safetensors")+".part")
	assert.Empty(t, r.svc.UserModels())
	assert.Empty(t, r.bundledModels())
}

func TestADownloadResumesFromThePartFile(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	r.hf.Add(hfRepo())
	dest := r.hfPath("checkpoints", "pony.safetensors")
	require.NoError(t, os.MkdirAll(dest[:strings.LastIndex(dest, "/")], 0o750))
	require.NoError(t, os.WriteFile(fetch.PartPath(dest), sdxlBody[:1000], 0o600))

	r.hfInstallOK("pony.safetensors")
	var ranged []string
	for _, q := range r.hf.Requests() {
		if strings.HasPrefix(q.Path, "/cdn/") && q.Range != "" {
			ranged = append(ranged, q.Range)
		}
	}
	assert.Contains(t, ranged, "bytes=1000-", "the part file was continued, not fetched again")
}

func TestNoRoomOnTheDiskRefusesBeforeAnyDownload(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	r.hf.Add(hfRepo())
	r.free = func(string) (uint64, error) { return uint64(len(sdxlBody)) - 1, nil }
	_, err := r.svc.InstallHFFile(ctx(), hfIn("pony.safetensors", "", ""))
	var ns *fetch.NoSpaceError
	require.ErrorAs(t, err, &ns)
	assert.Equal(t, uint64(len(sdxlBody)), ns.Need)
	assert.Zero(t, r.cdnRequests())
}

func TestTheInstallNeedsTheEngineAndAWritableSettingFile(t *testing.T) {
	r := newRig(t)
	r.hf.Add(hfRepo())
	_, err := r.svc.InstallHFFile(ctx(), hfIn("pony.safetensors", "", ""))
	require.ErrorIs(t, err, modelmanager.ErrEngineNotInstalled)
	p := newPinnedRig(t)
	p.installEngine()
	p.hf.Add(hfRepo())
	_, err = p.svc.InstallHFFile(ctx(), hfIn("pony.safetensors", "", ""))
	require.ErrorIs(t, err, modelmanager.ErrPinned)
	n := newRig(t, func(d *modelmanager.Deps, _ *rig) { d.Platform = "linux" })
	n.hf.Add(hfRepo())
	_, err = n.svc.InstallHFFile(ctx(), hfIn("pony.safetensors", "", ""))
	require.ErrorIs(t, err, modelmanager.ErrUnsupported)
}

func TestTheSameFileTwiceIsOneJobAndAnInstalledOneIsNotFetchedAgain(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	r.hf.Add(hfRepo())
	a, err := r.svc.InstallHFFile(ctx(), hfIn("pony.safetensors", "", ""))
	require.NoError(t, err)
	b, err := r.svc.InstallHFFile(ctx(), hfIn("pony.safetensors", "", ""))
	require.NoError(t, err)
	assert.Equal(t, a.JobID, b.JobID)
	assert.True(t, b.Existing)
	require.Equal(t, domain.JobSucceeded, r.wait(a.JobID).State)
	before := r.cdnRequests()
	r.hfInstallOK("pony.safetensors")
	assert.Equal(t, before, r.cdnRequests(), "the file in place is the pinned one: nothing is fetched")
	assert.Len(t, r.svc.UserModels(), 1)
}

func TestTheFolderAndTheNameAreTheUsersChoice(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	r.hf.Add(hfRepo())
	ref, err := r.svc.InstallHFFile(ctx(), hfIn("split_files/vae/ae.safetensors", "text_encoders", "my-ae.safetensors"))
	require.NoError(t, err)
	require.Equal(t, domain.JobSucceeded, r.wait(ref.JobID).State)
	assert.FileExists(t, r.hfPath("text_encoders", "my-ae.safetensors"))
	var invalid *modelmanager.InvalidError
	for _, in := range []modelmanager.HFInstallInput{
		hfIn("pony.safetensors", "../../x", ""), hfIn("pony.safetensors", "", "../x.safetensors"),
		hfIn("pony.safetensors", "", "x.ckpt"), hfIn("../pony.safetensors", "", ""),
		{Repo: "o/m", Commit: "main", Path: "pony.safetensors", Accept: true, TermsVersion: hfplan.TermsVersion},
	} {
		_, err := r.svc.InstallHFFile(ctx(), in)
		require.ErrorAs(t, err, &invalid, "%+v", in)
	}
	r.hf.Add(hftest.Repo{ID: "o/mystery", Commit: hfCommit, Files: []hftest.File{{Path: "w.safetensors", Body: hftest.Safetensors([]string{"foo"}, nil, 10)}}})
	in := hfIn("w.safetensors", "", "")
	in.Repo = "o/mystery"
	_, err = r.svc.InstallHFFile(ctx(), in)
	require.ErrorAs(t, err, &invalid, "a file whose folder cannot be told needs the user's choice")
	assert.Contains(t, invalid.Message, "folder")
	in.Folder = "diffusion_models"
	_, err = r.svc.InstallHFFile(ctx(), in)
	require.NoError(t, err)
}
