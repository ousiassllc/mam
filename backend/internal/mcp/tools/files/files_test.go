package files_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/files"
)

func newEnv(t *testing.T) *mcptest.Env {
	return mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) { files.Register(r, env.Services) })
}

func TestListUploadPurposes_DescribesWhatATicketNeeds(t *testing.T) {
	env := newEnv(t)
	var out files.ListPurposesOutput
	env.CallOK("list_upload_purposes", map[string]any{}, &out)

	byName := map[string]files.Purpose{}
	for _, p := range out.Purposes {
		byName[p.Purpose] = p
	}
	require.Contains(t, byName, "reference_image")
	require.Contains(t, byName, "import_bundle")
	var required []string
	for _, p := range byName["reference_image"].Params {
		if p.Required {
			required = append(required, p.Name)
		}
	}
	assert.ElementsMatch(t, []string{"project_id", "kind", "name"}, required)
	assert.EqualValues(t, 20<<20, byName["reference_image"].MaxBytes, "the limit of service/upload.go")
	assert.NotZero(t, byName["import_bundle"].MaxBytes)
}

func TestCreateUploadTicket_RefusesWhatTheTicketCannotBeBoundTo(t *testing.T) {
	env := newEnv(t)
	assert.Contains(t, env.CallError("create_upload_ticket", map[string]any{"purpose": "nope"}), "validation: invalid input: purpose")
	assert.Contains(t, env.CallError("create_upload_ticket", map[string]any{"purpose": "reference_image", "params": map[string]string{"kind": "style"}}), "project_id")
	assert.Contains(t, env.CallError("create_upload_ticket", map[string]any{"purpose": "import_bundle", "params": map[string]string{"x": "y"}}), "validation:")

	var ticket mcpkit.UploadTicket
	env.CallOK("create_upload_ticket", map[string]any{"purpose": "import_bundle"}, &ticket)
	assert.Equal(t, "POST", ticket.Method)
	assert.Equal(t, "file", ticket.FileField)
	assert.Contains(t, ticket.URL, mcpkit.FilesPath, "a relative URL when the request told no host")
}
