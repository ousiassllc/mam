package integration_test

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf/hftest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

func TestADifferentFileAtTheSamePlaceIsNeverOverwritten(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	r.hf.Add(hfRepo())
	r.hfInstallOK("pony.safetensors")

	other := hftest.Safetensors([]string{"model.diffusion_model.z", "conditioner.embedders.1.model.y", "first_stage_model.x"}, nil, 777)
	r.hf.Add(hftest.Repo{ID: "o/other", Commit: hfCommit, Files: []hftest.File{{Path: "pony.safetensors", Body: other}}})
	in := hfIn("pony.safetensors", "", "")
	in.Repo = "o/other"
	_, err := r.svc.InstallHFFile(ctx(), in)
	var invalid *modelmanager.InvalidError
	require.ErrorAs(t, err, &invalid, "a name that holds another file is asked about, not replaced")
	assert.Contains(t, invalid.Message, "上書きしません")
	assert.Contains(t, invalid.Message, "pony-"+hftest.SHA(other)[:8]+".safetensors", "another name is suggested")
	data, err := os.ReadFile(r.hfPath("checkpoints", "pony.safetensors"))
	require.NoError(t, err)
	assert.Equal(t, sdxlBody, data, "the file in place is untouched")
	assert.Len(t, r.svc.UserModels(), 1)

	in.Name = "pony-" + hftest.SHA(other)[:8] + ".safetensors"
	ref, err := r.svc.InstallHFFile(ctx(), in)
	require.NoError(t, err, "under another name it is installed beside the first")
	require.Equal(t, domain.JobSucceeded, r.wait(ref.JobID).State)
	assert.Len(t, r.svc.UserModels(), 2)
}

func TestAFileNobodyRecordedIsComparedByContentBeforeItIsReplaced(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	r.hf.Add(hfRepo())
	dest := r.hfPath("checkpoints", "pony.safetensors")
	require.NoError(t, os.MkdirAll(dest[:strings.LastIndex(dest, "/")], 0o750))

	require.NoError(t, os.WriteFile(dest, []byte("my own file"), 0o600))
	_, err := r.svc.InstallHFFile(ctx(), hfIn("pony.safetensors", "", ""))
	var invalid *modelmanager.InvalidError
	require.ErrorAs(t, err, &invalid, "another size")

	same := append([]byte(nil), sdxlBody...)
	same[len(same)-1] ^= 1
	require.NoError(t, os.WriteFile(dest, same, 0o600))
	_, err = r.svc.InstallHFFile(ctx(), hfIn("pony.safetensors", "", ""))
	require.ErrorAs(t, err, &invalid, "the same size and other bytes")
	got, _ := os.ReadFile(dest)
	assert.Equal(t, same, got)

	require.NoError(t, os.WriteFile(dest, sdxlBody, 0o600))
	r.hfInstallOK("pony.safetensors") // the pinned file is already there (a record that was lost): it is recorded, not replaced
	assert.Len(t, r.svc.UserModels(), 1)
	assert.Zero(t, r.cdnRequests())
}

// withHFAuth makes the fake Hugging Face the host that gets the token, as huggingface.co is in production (no other test lets
// a fake host see it).
func withHFAuth() option {
	return func(d *modelmanager.Deps, _ *rig) {
		anyURL := func(*url.URL) error { return nil }
		d.Fetch = fetch.Options{CheckURL: anyURL, Client: fetch.WithRedirectRules(&http.Client{}, anyURL, "127.0.0.1"), AuthHost: "127.0.0.1"}
	}
}

func TestAGatedRepositoryNeedsATokenAndGrantedAccessBeforeAnyDownload(t *testing.T) {
	r := newRig(t, withHFAuth())
	r.installEngine()
	repo := hfRepo()
	repo.Gated = true
	r.hf.Add(repo)

	_, err := r.svc.InstallHFFile(ctx(), hfIn("pony.safetensors", "", ""))
	var invalid *modelmanager.InvalidError
	require.ErrorAs(t, err, &invalid, "no token")
	assert.Contains(t, invalid.Message, "トークン")
	assert.Contains(t, invalid.Message, r.hf.URL+"/o/m")

	require.NoError(t, r.svc.SetHuggingFaceToken(r.hf.Token))
	_, err = r.svc.InstallHFFile(ctx(), hfIn("pony.safetensors", "", ""))
	require.ErrorAs(t, err, &invalid, "a token without access")
	assert.Contains(t, invalid.Message, "アクセスを申請")
	assert.Zero(t, r.cdnRequests(), "nothing was downloaded")
	assert.NotContains(t, readState(t, r), "hf_consents")

	r.hf.Granted = true
	r.hfInstallOK("pony.safetensors")
	assert.FileExists(t, r.hfPath("checkpoints", "pony.safetensors"))
	for _, q := range r.hf.Requests() {
		if q.Host == r.hf.CDNHost() {
			assert.Empty(t, q.Auth, "the token never goes to the CDN: %s", q.Path)
		}
	}
	assert.Equal(t, "Bearer "+r.hf.Token, lastAPIAuth(r), "the token goes to the Hugging Face host")
	assert.NotContains(t, readState(t, r), r.hf.Token, "the token is not in state.json")
	um := r.svc.UserModels()[0]
	assert.True(t, um.Gated)
}

func lastAPIAuth(r *rig) (auth string) {
	for _, q := range r.hf.Requests() {
		if q.Host != r.hf.CDNHost() && q.Auth != "" {
			auth = q.Auth
		}
	}
	return auth
}

func TestAnAdultRepositoryIsHiddenAndRefusedUntilTheAgeIsConfirmed(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	repo := hfRepo()
	repo.NFAA, repo.PipelineTag, repo.Downloads = true, "text-to-image", 10
	r.hf.Add(repo)
	r.hf.Add(hftest.Repo{ID: "o/m-clean", Commit: hfCommit, PipelineTag: "text-to-image", Downloads: 1})

	hits, err := r.svc.SearchHF(ctx(), "o/m", "text-to-image", 20)
	require.NoError(t, err)
	require.Len(t, hits, 1, "the NFAA repository is not in the results")
	assert.Equal(t, "o/m-clean", hits[0].Repo)
	_, err = r.svc.HFRepo(ctx(), "o/m")
	require.ErrorIs(t, err, modelmanager.ErrAdultNotConfirmed)
	treeBefore := r.hf.Count("/api/models/o/m/tree/")
	_, err = r.svc.InstallHFFile(ctx(), hfIn("pony.safetensors", "", ""))
	require.ErrorIs(t, err, modelmanager.ErrAdultNotConfirmed, "even with the terms accepted in the same call")
	assert.Equal(t, treeBefore, r.hf.Count("/api/models/o/m/tree/"))
	assert.Zero(t, r.cdnRequests())
	assert.NotContains(t, readState(t, r), "hf_consents", "nothing was recorded")

	r.confirmAdult()
	hits, err = r.svc.SearchHF(ctx(), "o/m", "text-to-image", 20)
	require.NoError(t, err)
	assert.Len(t, hits, 2)
	assert.True(t, hits[0].NFAA)
	v, err := r.svc.HFRepo(ctx(), "o/m")
	require.NoError(t, err)
	assert.True(t, v.Repo.NFAA)
	r.hfInstallOK("pony.safetensors")
	require.Len(t, r.svc.UserModels(), 1)
	wf := r.svc.UserModels()[0].ModelIDs[0]
	assert.False(t, r.svc.HidesModel(wf), "the gate is open")

	require.NoError(t, r.svc.SetAdultContent(false, nil))
	assert.Empty(t, r.svc.UserModels(), "an NFAA file is not listed while the gate is closed")
	assert.True(t, r.svc.HidesModel(wf), "its models are hidden from the trial generation")
	require.NoError(t, r.svc.DeleteUserModel(ctx(), "models/checkpoints/pony.safetensors"), "clearing files always works")
	assert.NoFileExists(t, r.hfPath("checkpoints", "pony.safetensors"))
}

func TestTheGateClosedAfterTheJobWasQueuedFailsTheJob(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	repo := hfRepo()
	repo.NFAA = true
	r.hf.Add(repo)
	r.confirmAdult()
	ref, err := r.svc.InstallHFFile(ctx(), hfIn("pony.safetensors", "", ""))
	require.NoError(t, err)
	require.NoError(t, r.svc.SetAdultContent(false, nil)) // the queue has not started: the job is waiting
	st := r.wait(ref.JobID)
	require.Equal(t, domain.JobFailed, st.State)
	assert.Contains(t, st.Error, "成人向け")
	assert.Zero(t, r.cdnRequests())
	assert.NoFileExists(t, r.hfPath("checkpoints", "pony.safetensors"))
}
