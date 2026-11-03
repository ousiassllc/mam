package runtimemodels_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/runtimemodels"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime/lrtest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/service/runtimechoices/choicetest"
)

func TestMain(m *testing.M) {
	if lrtest.ServeIfAsked() {
		return
	}
	mmtest.Main(m)
}

// newRuntimeEnv is the MCP environment with the local runtimes over fakes (a Windows PC with an RTX 4070 unless an option says another),
// the test catalog's runtime having two models (choicetest.WithChoices).
func newRuntimeEnv(t *testing.T, opts ...lrtest.Option) (*mcptest.Env, *mmtest.Rig, *lrtest.Rig) {
	t.Helper()
	var mm *mmtest.Rig
	var lr *lrtest.Rig
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		mm = mmtest.New(t, mmtest.WithDB(env.DB), mmtest.WithLogger(env.Logger))
		lr = lrtest.New(t, mm.Queue, mm.Table, mm.Svc.HandleInstall, opts...)
		env.Services.ModelManager, env.Services.LocalRuntimes = mm.Svc, lr.Svc
		runtimemodels.Register(r, env.Services)
	})
	return env, mm, lr
}

func modelOf(t *testing.T, out runtimemodels.ListOutput, id string) (found bool, active, needed, accepted bool) {
	t.Helper()
	for _, m := range out.Models {
		if m.ID == id {
			return true, m.Active, m.ConsentNeeded, m.Accepted
		}
	}
	return false, false, false, false
}

func TestRuntimeModels_ListNamesTheModelsTheirLicensesAndWhichOneIsInForce(t *testing.T) {
	env, _, _ := newRuntimeEnv(t, choicetest.WithChoices())

	var out runtimemodels.ListOutput
	env.CallOK("list_local_runtime_models", map[string]any{"runtime_id": lrtest.ID}, &out)

	require.Len(t, out.Models, 2)
	found, active, needed, _ := modelOf(t, out, choicetest.SmallChoice)
	assert.True(t, found)
	assert.True(t, active)
	assert.False(t, needed)
	_, active, needed, accepted := modelOf(t, out, choicetest.LargeChoice)
	assert.False(t, active)
	assert.True(t, needed)
	assert.False(t, accepted)
	assert.Equal(t, "tts", out.Models[0].Kind)
	assert.Equal(t, []string{"ja"}, out.Models[0].Languages)
	assert.NotEmpty(t, out.Models[1].Cautions)
	require.NotNil(t, out.Models[1].Consent)
	assert.Equal(t, choicetest.LargeConsentVersion, out.Models[1].Consent.Version)
	assert.Contains(t, env.CallError("list_local_runtime_models", map[string]any{"runtime_id": "nope"}), "not_found")
}

func TestRuntimeModels_AModelWhoseLicenseWasNotAcceptedIsRefusedAndNothingIsRecorded(t *testing.T) {
	env, _, _ := newRuntimeEnv(t, choicetest.WithChoices())

	msg := env.CallError("select_local_runtime_model", map[string]any{"runtime_id": lrtest.ID, "model_id": choicetest.LargeChoice})
	wrong := env.CallError("select_local_runtime_model", map[string]any{"runtime_id": lrtest.ID, "model_id": choicetest.LargeChoice, "accept_license_version": "terms-0"})

	assert.Contains(t, msg, "conflict")
	assert.Contains(t, wrong, "conflict")
	var out runtimemodels.ListOutput
	env.CallOK("list_local_runtime_models", map[string]any{"runtime_id": lrtest.ID}, &out)
	_, active, _, accepted := modelOf(t, out, choicetest.LargeChoice)
	assert.False(t, active)
	assert.False(t, accepted)
}

func TestRuntimeModels_TheUsersAcceptanceOfTheShownVersionChoosesTheModel(t *testing.T) {
	env, _, _ := newRuntimeEnv(t, choicetest.WithChoices())

	var out runtimemodels.ListOutput
	env.CallOK("select_local_runtime_model", map[string]any{"runtime_id": lrtest.ID, "model_id": choicetest.LargeChoice, "accept_license_version": choicetest.LargeConsentVersion}, &out)

	_, active, needed, accepted := modelOf(t, out, choicetest.LargeChoice)
	assert.True(t, active)
	assert.False(t, needed)
	assert.True(t, accepted)
	env.CallOK("select_local_runtime_model", map[string]any{"runtime_id": lrtest.ID, "model_id": choicetest.SmallChoice}, &out)
	_, active, _, _ = modelOf(t, out, choicetest.SmallChoice)
	assert.True(t, active)
}

func TestRuntimeModels_AnUnknownModelIsAValidationError(t *testing.T) {
	env, _, _ := newRuntimeEnv(t, choicetest.WithChoices())

	assert.Contains(t, env.CallError("select_local_runtime_model", map[string]any{"runtime_id": lrtest.ID, "model_id": "nope"}), "validation")
}

func TestRuntimeModels_UnavailableWithoutTheService(t *testing.T) {
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) { runtimemodels.Register(r, env.Services) })

	assert.Contains(t, env.CallError("list_local_runtime_models", map[string]any{"runtime_id": "irodori-tts"}), "unavailable")
	assert.Contains(t, env.CallError("select_local_runtime_model", map[string]any{"runtime_id": "irodori-tts", "model_id": "v4.1-small"}), "unavailable")
}
