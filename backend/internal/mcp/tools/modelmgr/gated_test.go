package modelmgr_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/hfguide"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

// install_model of a model behind the Hugging Face login (Issue #877) tells the same as the screen and the service in the four
// cases (validation before any job; the job once the access is granted), and the token is in none of the answers or the catalog.
func TestInstallModel_AModelBehindTheLoginChecksTheTokenAndTheAccessBeforeAJob(t *testing.T) {
	env, rig := newEnv(t, mmtest.WithGatedModel(), mmtest.HFAuth())
	rig.InstallEngine()
	token := rig.HF.Token
	args := map[string]any{"model_id": mmtest.GatedModelID, "license_id": mmtest.GatedLicense.ID, "license_version": mmtest.GatedLicense.Version}
	var errs []string

	msg := env.CallError("install_model", args) // no token
	errs = append(errs, msg)
	assert.Contains(t, msg, "validation")
	assert.Contains(t, msg, hfguide.NoToken)

	env.CallOK("set_huggingface_token", map[string]any{"token": "hf_notthetokenthehubknows"}, &modelmgr.TokenStatus{})
	msg = env.CallError("install_model", args) // a token the Hub does not accept
	errs = append(errs, msg)
	assert.Contains(t, msg, "validation")
	assert.Contains(t, msg, hfguide.InvalidToken)

	env.CallOK("set_huggingface_token", map[string]any{"token": token}, &modelmgr.TokenStatus{})
	msg = env.CallError("install_model", args) // a token without the access
	errs = append(errs, msg)
	assert.Contains(t, msg, "validation")
	assert.Contains(t, msg, "ライセンスに同意してアクセスを申請")
	assert.Contains(t, msg, rig.HF.URL+"/"+mmtest.GatedRepo)
	assert.Zero(t, rig.HF.Count("/cdn/"), "nothing was downloaded")

	rig.HF.Granted = true
	var job modelmgr.JobOutput
	env.CallOK("install_model", args, &job)
	assert.Equal(t, mcpkit.JobSucceeded, jobDone(t, env, rig, job.JobID).State)
	assert.FileExists(t, rig.ModelFile(mmtest.GatedFile))

	logged, err := os.ReadFile(filepath.Join(env.LogDir, "mam.log")) // the log has neither token, however the install went
	if err == nil {
		assert.NotContains(t, string(logged), token)
		assert.NotContains(t, string(logged), "hf_notthetokenthehubknows")
	}
	var status modelmgr.TokenStatus
	env.CallOK("get_huggingface_token_status", map[string]any{}, &status)
	require.True(t, status.Configured)
	for _, m := range append(errs, status.LastChars) {
		assert.NotContains(t, m, token)
		assert.NotContains(t, m, "hf_notthetokenthehubknows")
	}
	var cat modelmgr.ListCatalogOutput
	env.CallOK("list_model_catalog", map[string]any{}, &cat)
	found := false
	for _, m := range cat.Models {
		if m.ID == mmtest.GatedModelID {
			found = true
			assert.True(t, m.RequiresHFToken, "the catalog says the model needs the token")
		}
	}
	assert.True(t, found)
}
