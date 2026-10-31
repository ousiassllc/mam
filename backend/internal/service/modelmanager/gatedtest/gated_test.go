// Package gatedtest runs the install of a model that is behind a Hugging Face login (catalog requires_hf_token; Issue #877: LTX-2.5)
// over the model manager's rig and the fake Hugging Face (spec/architecture/model-manager/model-manager.md §5.3): the check of the
// token and of the access before any job, in the four ways (no token, a token Hugging Face does not accept, an access that was not
// granted, success), and the refusal half way. The token is in none of the messages, state.json or the jobs.
package gatedtest_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/hfguide"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

func TestMain(m *testing.M) { mmtest.Main(m) }

const wrongToken = "hf_wrongtoken0123456789"

func newRig(t *testing.T) *mmtest.Rig {
	t.Helper()
	r := mmtest.New(t, mmtest.WithGatedModel(), mmtest.HFAuth())
	r.InstallEngine()
	return r
}

// install asks for the install of m-g (with the acceptance of its license) and returns the refusal message ("" when the job was made).
func install(t *testing.T, r *mmtest.Rig) (modelmanager.JobRef, string) {
	t.Helper()
	ref, err := r.Svc.InstallModel(context.Background(), mmtest.GatedModelID, &mmtest.GatedLicense)
	if err == nil {
		return ref, ""
	}
	var invalid *modelmanager.InvalidError
	require.ErrorAs(t, err, &invalid, "a guidance, not a fault: %v", err)
	return ref, invalid.Message
}

// noJob checks that the refusal made no job and downloaded nothing.
func noJob(t *testing.T, r *mmtest.Rig) {
	t.Helper()
	m, err := r.Svc.Model(mmtest.GatedModelID)
	require.NoError(t, err)
	assert.Empty(t, m.InstallJob, "no job")
	assert.Zero(t, r.HF.Count("/cdn/"), "nothing was downloaded")
}

func state(t *testing.T, r *mmtest.Rig) string {
	t.Helper()
	data, err := os.ReadFile(r.Layout.StatePath())
	require.NoError(t, err)
	return string(data)
}

func TestAGatedCatalogModelChecksTheTokenAndTheAccessBeforeAnyJob(t *testing.T) {
	r := newRig(t)
	token := r.HF.Token
	var told []string

	// 1. no token: A, before the consent is recorded, and Hugging Face is not even asked
	_, msg := install(t, r)
	told = append(told, msg)
	assert.Equal(t, hfguide.NoToken, msg)
	assert.Contains(t, msg, "/model-manager#mm-hf", "the link to the token card")
	assert.Zero(t, r.HF.Count("/"+mmtest.GatedRepo))
	assert.NotContains(t, state(t, r), "consents", "no consent for a model that cannot be fetched")
	noJob(t, r)

	// 2. a token Hugging Face does not accept (401): B
	require.NoError(t, r.Svc.SetHuggingFaceToken(wrongToken))
	_, msg = install(t, r)
	told = append(told, msg)
	assert.Equal(t, hfguide.InvalidToken, msg)
	assert.Contains(t, msg, "発行し直し")
	noJob(t, r)

	// 3. a good token without the access (403 GatedRepo): C, with the page of the model; the access is never asked for through the API
	require.NoError(t, r.Svc.SetHuggingFaceToken(token))
	_, msg = install(t, r)
	told = append(told, msg)
	assert.Contains(t, msg, "ライセンスに同意してアクセスを申請")
	assert.Contains(t, msg, r.HF.URL+"/"+mmtest.GatedRepo, "the page of the model")
	assert.Contains(t, msg, "mam からは申請できません")
	noJob(t, r)
	for _, q := range r.HF.Requests() {
		assert.Equal(t, "GET", q.Method, "only reads: %s", q.Path)
	}

	// 4. the access granted: the first byte is asked for (and never the whole file), then the job is made and the file fetched
	r.HF.Granted.Store(true)
	ref, msg := install(t, r)
	require.Empty(t, msg)
	st := r.Wait(ref.JobID)
	require.Equal(t, domain.JobSucceeded, st.State, st.Error)
	assert.FileExists(t, r.ModelFile(mmtest.GatedFile))
	probes := 0
	for _, q := range r.HF.Requests() {
		if q.Range == "bytes=0-0" && strings.HasPrefix(q.Path, "/"+mmtest.GatedRepo+"/resolve/"+mmtest.GatedCommit+"/") {
			probes++
		}
		if q.Host == r.HF.CDNHost() {
			assert.Empty(t, q.Auth, "the token never goes to the CDN: %s", q.Path)
		}
	}
	assert.Positive(t, probes, "the access was checked with the first byte of the file")

	for _, m := range told {
		assert.NotContains(t, m, token)
		assert.NotContains(t, m, wrongToken)
	}
	assert.NotContains(t, state(t, r), token, "the token is not in state.json")
}

// The answer of Hugging Face half way (the access withdrawn, the token revoked) after the job was queued fails the job with the same
// guidance as the check before it.
func TestAGatedDownloadRefusedHalfWayFailsWithTheSameGuidance(t *testing.T) {
	r := mmtest.New(t, mmtest.WithGatedModel(), mmtest.HFAuth())
	hold := newQueueHold(r)
	r.InstallEngine()
	token := r.HF.Token
	require.NoError(t, r.Svc.SetHuggingFaceToken(token))
	r.HF.Granted.Store(true)

	release := hold.Hold(t, r) // every worker is busy: the job waits in the queue
	ref, msg := install(t, r)
	require.Empty(t, msg)
	r.HF.Granted.Store(false) // the access is withdrawn after the check: 403 (C)
	release()
	st := r.Wait(ref.JobID)
	require.Equal(t, domain.JobFailed, st.State)
	assert.Contains(t, st.Error, "ライセンスに同意してアクセスを申請")
	assert.Contains(t, st.Error, r.HF.URL+"/"+mmtest.GatedRepo)
	assert.NotContains(t, st.Error, token)
	assert.NoFileExists(t, r.ModelFile(mmtest.GatedFile))

	r.HF.Granted.Store(true)
	release = hold.Hold(t, r)
	ref, msg = install(t, r)
	require.Empty(t, msg)
	require.NoError(t, r.Svc.SetHuggingFaceToken(wrongToken)) // the token is revoked after the check: 401 (B)
	release()
	st = r.Wait(ref.JobID)
	require.Equal(t, domain.JobFailed, st.State)
	assert.Equal(t, hfguide.InvalidToken, st.Error)
	assert.NotContains(t, st.Error, wrongToken)
	assert.NotContains(t, state(t, r), wrongToken)
}

// A model that is not behind the login is installed without any token and without a probe.
func TestAModelWithoutTheLoginNeedsNoTokenAndNoProbe(t *testing.T) {
	r := newRig(t)
	st := r.InstallModel("m-a")
	require.Equal(t, domain.JobSucceeded, st.State, st.Error)
	assert.Zero(t, r.HF.Count("/"), "Hugging Face was not asked for a model without the login")
}
