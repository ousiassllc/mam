package gentest_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	mcpgentest "github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/projectlink"
)

func TestUploadedReferenceIsUsedByTheBatchAndCheckedAsAPicture(t *testing.T) {
	r := newRig(t)
	var ticket mcpkit.UploadTicket
	r.CallOK("create_upload_ticket", map[string]any{"purpose": mcpgentest.PurposeReference}, &ticket)
	assert.Equal(t, int64(20<<20), ticket.MaxBytes)

	bad, ct := multipartBody(t, ticket.FileField, "ref.png", []byte("GIF89a not what it says"))
	rec := r.serve(http.MethodPost, ticket.URL, bad, ct)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())

	r.CallOK("create_upload_ticket", map[string]any{"purpose": mcpgentest.PurposeReference}, &ticket)
	body, ct := multipartBody(t, ticket.FileField, "ref.png", gentesttest.PNG)
	up := r.serve(http.MethodPost, ticket.URL, body, ct)
	require.Equal(t, http.StatusCreated, up.Code, up.Body.String())
	refID := strings.Split(strings.SplitN(up.Body.String(), `"reference_id":"`, 2)[1], `"`)[0]
	require.NotEmpty(t, refID)

	args := map[string]any{
		"media": "image", "models": []string{"flux-2"}, "prompts": []map[string]any{{"prompt": "fox"}},
		"references": []map[string]any{{"source": "upload", "id": refID}},
	}
	out, _ := r.run(args)
	r.job(out.JobID)
	var b gentest.BatchStatus
	r.CallOK("get_generation_test", map[string]any{"batch_id": out.BatchID}, &b)
	var got mcpgentest.ResultOutput
	r.CallOK("get_generation_result", map[string]any{"result_id": b.Results[0].ID}, &got)
	require.Len(t, got.Inputs, 1, "the picture is kept with the batch")
	assert.Contains(t, r.CallError("estimate_generation_test", map[string]any{
		"media": "image", "models": []string{"flux-2"}, "prompts": []map[string]any{{"prompt": "fox"}},
		"references": []map[string]any{{"source": "upload", "id": "../../etc/passwd"}},
	}), "validation", "an input that is not there is refused at the estimate")
}

func TestImportIntoAProjectAsAReferenceAndAssignedToASlot(t *testing.T) {
	r := newRig(t)
	out, _ := r.run(map[string]any{"media": "image", "models": []string{"flux-2"}, "prompts": []map[string]any{{"prompt": "fox"}}})
	r.job(out.JobID)
	var b gentest.BatchStatus
	r.CallOK("get_generation_test", map[string]any{"batch_id": out.BatchID}, &b)
	project := r.GT.NewProject()

	var imp projectlink.ImportOutcome
	r.CallOK("import_generation_result", map[string]any{
		"result_id": b.Results[0].ID, "project_id": project, "as": "reference", "kind": "character", "name": "Mio",
		"assign": map[string]any{"target": "character", "id": "c1", "slot": "face"},
	}, &imp)
	assert.NotEmpty(t, imp.ReferenceItemID)
	assert.True(t, imp.Assigned)
	assert.Equal(t, []string{project + "|character|c1|face|" + imp.ReferenceItemID}, *r.GT.Assigned)

	r.CallOK("import_generation_result", map[string]any{"result_id": b.Results[0].ID, "project_id": project, "as": "asset"}, &imp)
	assert.NotEmpty(t, imp.AssetID)
	assert.Contains(t, r.CallError("import_generation_result", map[string]any{"result_id": b.Results[0].ID, "project_id": "nope", "as": "asset"}), "not_found")
	assert.Contains(t, r.CallError("import_generation_result", map[string]any{"result_id": "nope", "project_id": project, "as": "asset"}), "not_found")
}

func TestImportWhoseSlotIsLockedKeepsTheImageAndSaysSo(t *testing.T) {
	r := newRig(t, gentesttest.WithAssignError(service.ErrLocked))
	out, _ := r.run(map[string]any{"media": "image", "models": []string{"flux-2"}, "prompts": []map[string]any{{"prompt": "fox"}}})
	r.job(out.JobID)
	var b gentest.BatchStatus
	r.CallOK("get_generation_test", map[string]any{"batch_id": out.BatchID}, &b)
	project := r.GT.NewProject()

	msg := r.CallError("import_generation_result", map[string]any{
		"result_id": b.Results[0].ID, "project_id": project, "as": "reference", "kind": "character", "name": "Mio",
		"assign": map[string]any{"target": "character", "id": "c1", "slot": "face"},
	})
	assert.Contains(t, msg, "locked")
	assert.Contains(t, msg, "取り込みました")
	var n int
	require.NoError(t, r.DB.Reader().QueryRow("SELECT COUNT(*) FROM reference_item").Scan(&n))
	assert.Equal(t, 1, n, "the Library image exists")
}

func TestGetJobReadsABatchStartedElsewhere(t *testing.T) {
	r := newRig(t)
	ref, err := r.GT.Confirmed(gentest.Request{Media: "image", Models: []string{"flux-2"}, Prompts: []gentest.PromptInput{{Prompt: "from the screen"}}})
	require.NoError(t, err)
	st := r.job(ref.JobID)
	assert.Equal(t, "gentest", st.Kind)
	require.Equal(t, mcpkit.JobSucceeded, st.State, st.Error)
	assert.Equal(t, ref.BatchID, st.Result["batch_id"])

	require.NoError(t, r.GT.Svc.DeleteBatch(t.Context(), ref.BatchID))
	gone := r.job(ref.JobID)
	assert.Equal(t, mcpkit.JobCancelled, gone.State, "the batch was deleted")
}

func TestToolsAreUnavailableWithoutAGenerationTest(t *testing.T) {
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) { mcpgentest.Register(r, env.Services) })
	assert.Contains(t, env.CallError("list_generation_models", map[string]any{}), "unavailable")
	assert.Contains(t, env.CallError("list_generation_results", map[string]any{}), "unavailable")
}
