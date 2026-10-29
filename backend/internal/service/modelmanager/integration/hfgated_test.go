package integration_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf/hftest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// The gated model of the Hugging Face login (Issue #877: LTX-2.5 is behind it). Its files are those of a gated repository of the
// fake Hugging Face; the model needs the license consent, too.
const (
	gatedRepoID     = "Lightricks/LTX-2.5"
	gatedCommit     = "49b093a39c19169a8a81721dcc541e07e945232f"
	gatedWrongToken = "hf_wrongtoken0123456789"
)

var gatedBody = []byte(strings.Repeat("G", 120_000))

// withGatedModel adds the model m-g (requires_hf_token, consent required) to the catalog, with its file in a gated repository.
func withGatedModel() option {
	return func(d *modelmanager.Deps, r *rig) {
		r.hf.Add(hftest.Repo{ID: gatedRepoID, Commit: gatedCommit, Gated: true, Files: []hftest.File{{Path: "vae/g.safetensors", Body: gatedBody}}})
		d.Catalog.Models = append(d.Catalog.Models, catalog.Model{
			ID: "m-g", Name: "G", Kind: localcfg.MediaImage, Tier: catalog.TierAdvanced, Description: "d", RequiresHFToken: true, ConsentRequired: true,
			VRAM:    catalog.VRAM{MinGB: 8, Source: "test", Estimate: true},
			License: catalog.License{ID: "gated", Version: "1", Name: "gated", URL: "https://example.com/gated", Summary: "s", CommercialUse: true, CommercialCondition: "c"},
			Files: []catalog.File{{
				Name: "g.safetensors", Folder: "vae", URL: r.hf.URL + "/" + gatedRepoID + "/resolve/" + gatedCommit + "/vae/g.safetensors",
				SHA256: sumOf(gatedBody), Size: int64(len(gatedBody)),
			}},
			Workflows: []catalog.Workflow{{Name: "wf-g", Template: "txt2img-sdxl"}},
		})
	}
}

var gatedLicense = &modelmanager.LicenseRef{ID: "gated", Version: "1"}

// installG asks for the install of m-g and returns the refusal message ("" when the job was made).
func (r *rig) installG() (modelmanager.JobRef, string) {
	r.t.Helper()
	ref, err := r.svc.InstallModel(ctx(), "m-g", gatedLicense)
	if err == nil {
		return ref, ""
	}
	var invalid *modelmanager.InvalidError
	require.ErrorAs(r.t, err, &invalid, "a guidance, not a fault: %v", err)
	return ref, invalid.Message
}

// noJob checks that the refusal made no job and downloaded nothing.
func (r *rig) noJob() {
	r.t.Helper()
	m, err := r.svc.Model("m-g")
	require.NoError(r.t, err)
	assert.Empty(r.t, m.InstallJob, "no job")
	assert.Zero(r.t, r.cdnRequests(), "nothing was downloaded")
}

// A model behind the login refuses before any job in the four ways: no token (message A, before the consent is recorded), a token
// Hugging Face does not accept (B, 401), an access that was not granted (C, 403 GatedRepo, with the page of the model), and the
// success. The value of the token is in none of the messages, state.json or the failure of a job.
func TestAGatedCatalogModelChecksTheTokenAndTheAccessBeforeAnyJob(t *testing.T) {
	r := newRig(t, withHFAuth(), withGatedModel())
	r.installEngine()
	token := r.hf.Token
	var told []string

	// 1. no token: A, and the consent is not recorded
	_, msg := r.installG()
	told = append(told, msg)
	assert.Equal(t, modelmanager.HFNoTokenGuide, msg)
	assert.Contains(t, msg, "/model-manager#mm-hf", "the link to the token card")
	assert.Zero(t, r.hf.Count("/"+gatedRepoID), "Hugging Face was not even asked")
	assert.NotContains(t, readState(t, r), "consents", "no consent for a model that cannot be fetched")
	r.noJob()

	// 2. a token Hugging Face does not accept: B
	require.NoError(t, r.svc.SetHuggingFaceToken(gatedWrongToken))
	_, msg = r.installG()
	told = append(told, msg)
	assert.Equal(t, modelmanager.HFInvalidTokenGuide, msg)
	assert.Contains(t, msg, "発行し直し")
	r.noJob()

	// 3. a good token without access: C, with the page of the model (an access is requested there, never through the API)
	require.NoError(t, r.svc.SetHuggingFaceToken(token))
	_, msg = r.installG()
	told = append(told, msg)
	assert.Contains(t, msg, "ライセンスに同意してアクセスを申請")
	assert.Contains(t, msg, r.hf.URL+"/"+gatedRepoID, "the page of the model")
	assert.Contains(t, msg, "mam からは申請できません")
	r.noJob()
	for _, q := range r.hf.Requests() {
		assert.NotEqual(t, "POST", q.Method, "no request for the access through the API: %s", q.Path)
	}

	// 4. access granted: the first byte is asked for, then the job is made and the file is fetched
	r.hf.Granted = true
	ref, msg := r.installG()
	require.Empty(t, msg)
	st := r.wait(ref.JobID)
	require.Equal(t, domain.JobSucceeded, st.State, st.Error)
	assert.FileExists(t, r.hfPath("vae", "g.safetensors"))
	probes := 0
	for _, q := range r.hf.Requests() {
		if q.Range == "bytes=0-0" && strings.HasPrefix(q.Path, "/"+gatedRepoID+"/resolve/"+gatedCommit+"/") {
			probes++
		}
		if q.Host == r.hf.CDNHost() {
			assert.Empty(t, q.Auth, "the token never goes to the CDN: %s", q.Path)
		}
	}
	assert.Positive(t, probes, "the access was checked with the first byte of the file")

	for _, m := range told {
		assert.NotContains(t, m, token)
		assert.NotContains(t, m, gatedWrongToken)
	}
	assert.NotContains(t, readState(t, r), token, "the token is not in state.json")
	m, err := r.svc.Model("m-g")
	require.NoError(t, err)
	assert.True(t, m.RequiresHFToken)
}

// The answer of Hugging Face half way (the access withdrawn, the token revoked) after the job was queued fails the job with the same
// guidance as the check before it.
func TestAGatedDownloadRefusedHalfWayFailsWithTheSameGuidance(t *testing.T) {
	r := newRig(t, withHFAuth(), withGatedModel())
	r.installEngine()
	token := r.hf.Token
	require.NoError(t, r.svc.SetHuggingFaceToken(token))
	r.hf.Granted = true

	// the access is withdrawn after the check: 403 (C)
	ref, msg := r.installG() // the queue is not running yet: the job waits
	require.Empty(t, msg)
	r.hf.Granted = false
	st := r.wait(ref.JobID)
	require.Equal(t, domain.JobFailed, st.State)
	assert.Contains(t, st.Error, "ライセンスに同意してアクセスを申請")
	assert.Contains(t, st.Error, r.hf.URL+"/"+gatedRepoID)
	assert.NotContains(t, st.Error, token)
	assert.NoFileExists(t, r.hfPath("vae", "g.safetensors"))

	// the token is revoked after the check: 401 (B)
	r.hf.Granted = true
	ref, msg = r.installG()
	require.Empty(t, msg)
	require.NoError(t, r.svc.SetHuggingFaceToken(gatedWrongToken))
	st = r.wait(ref.JobID)
	require.Equal(t, domain.JobFailed, st.State)
	assert.Equal(t, modelmanager.HFInvalidTokenGuide, st.Error)
	assert.NotContains(t, st.Error, gatedWrongToken)
	assert.NotContains(t, readState(t, r), gatedWrongToken)
}

// A model that is not behind the login is installed without any token and without a probe.
func TestAModelWithoutTheLoginNeedsNoTokenAndNoProbe(t *testing.T) {
	r := newRig(t, withHFAuth(), withGatedModel())
	r.installEngine()
	st := r.installModel("m-a")
	require.Equal(t, domain.JobSucceeded, st.State, st.Error)
	assert.Zero(t, r.hf.Count("/"), "Hugging Face was not asked for a model without the login")
}
