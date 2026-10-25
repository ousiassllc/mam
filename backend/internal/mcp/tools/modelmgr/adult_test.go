package modelmgr_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

func catalogIDs(env interface {
	CallOK(string, any, any)
}) []string {
	var cat modelmgr.ListCatalogOutput
	env.CallOK("list_model_catalog", map[string]any{}, &cat)
	var ids []string
	for _, m := range cat.Models {
		ids = append(ids, m.ID)
	}
	return ids
}

// The MCP tools go through the same Service: an adult model is not listed, not readable, not accepted and not installed until
// the user's confirmation is on record, and the agent cannot make one up (confirm_adult must be true and carry the wording's version).
func TestAdult_MCPHidesAndRefusesUntilTheUserConfirmed(t *testing.T) {
	env, rig := newEnv(t)
	rig.InstallEngine()
	assert.Equal(t, []string{"m-a", "m-b", "m-c"}, catalogIDs(env))
	requests := rig.Host.Requests()

	for _, call := range []struct {
		tool string
		args map[string]any
	}{
		{"get_catalog_model", map[string]any{"model_id": "m-d"}},
		{"install_model", map[string]any{"model_id": "m-d"}},
		{"install_model", map[string]any{"model_id": "m-d", "license_id": "unspecified", "license_version": "1"}},
		{"accept_model_license", map[string]any{"model_id": "m-d", "license_id": "unspecified", "license_version": "1"}},
	} {
		msg := env.CallError(call.tool, call.args)
		assert.Contains(t, msg, "conflict", call.tool)
		assert.Contains(t, msg, "成人向けモデルです", call.tool)
	}
	assert.Equal(t, requests, rig.Host.Requests(), "nothing was downloaded")

	var st modelmanager.AdultContentStatus
	env.CallOK("get_adult_content_status", map[string]any{}, &st)
	assert.False(t, st.Enabled)
	assert.False(t, st.Confirmed)
	assert.Equal(t, modelmanager.AdultNoticeVersion, st.NoticeVersion)
	assert.Contains(t, st.Notice, "未成年（未成年に見える表現を含む）")

	// turning it on without the user's confirmation, or with a wording the user did not see, is refused
	assert.Contains(t, env.CallError("set_adult_content", map[string]any{"enabled": true}), "conflict")
	assert.Contains(t, env.CallError("set_adult_content", map[string]any{"enabled": true, "confirm_adult": false, "notice_version": modelmanager.AdultNoticeVersion}), "validation")
	assert.Contains(t, env.CallError("set_adult_content", map[string]any{"enabled": true, "confirm_adult": true}), "validation")
	assert.Contains(t, env.CallError("set_adult_content", map[string]any{"enabled": true, "confirm_adult": true, "notice_version": "0"}), "validation")
	assert.False(t, rig.Svc.AdultContentEnabled())
	assert.Equal(t, []string{"m-a", "m-b", "m-c"}, catalogIDs(env))

	env.CallOK("set_adult_content", map[string]any{"enabled": true, "confirm_adult": true, "notice_version": modelmanager.AdultNoticeVersion}, &st)
	assert.True(t, st.Enabled)
	assert.True(t, st.Confirmed)
	require.NotNil(t, st.ConfirmedAt)
	assert.Equal(t, []string{"m-a", "m-b", "m-c", "m-d"}, catalogIDs(env))

	var one modelmanager.ModelView
	env.CallOK("get_catalog_model", map[string]any{"model_id": "m-d"}, &one)
	assert.Equal(t, "adult", string(one.ContentRating))
	assert.Equal(t, []string{"nsfwsks"}, one.TriggerWords)
	assert.Equal(t, mmtest.AdultNoticeSource, one.Files[1].SourceNote)
	assert.Equal(t, modelmanager.AdultNotice, one.AdultNotice)

	// the license consent is still separate: the install is a conflict until it is given
	assert.Contains(t, env.CallError("install_model", map[string]any{"model_id": "m-d"}), "ライセンスに同意してから導入してください")
	var out modelmgr.JobOutput
	env.CallOK("install_model", map[string]any{"model_id": "m-d", "license_id": "unspecified", "license_version": "1"}, &out)
	done := jobDone(t, env, rig, out.JobID)
	assert.Equal(t, mcpkit.JobSucceeded, done.State, done.Error)

	env.CallOK("set_adult_content", map[string]any{"enabled": false}, &st)
	assert.False(t, st.Enabled)
	assert.True(t, st.Confirmed, "the confirmation is kept")
	assert.Equal(t, []string{"m-a", "m-b", "m-c"}, catalogIDs(env))
}
