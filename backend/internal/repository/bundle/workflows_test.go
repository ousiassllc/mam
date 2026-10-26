package bundle_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/bundle"
)

const workflowID = "0199d7a0-1111-7222-8333-444455556666"

// A bundle does not carry workflows (they are global to the instance): an import still succeeds, and the chat of
// the imported Project says which saved workflows its Takes and Video Model locks name that this instance lacks.
func TestBundleRepository_ImportWarnsAboutMissingWorkflowsInsteadOfFailing(t *testing.T) {
	ctx := context.Background()
	source := openTestDB(t)
	seedDeepProject(t, source, "a")
	v1, v2 := modelreg.WorkflowID(workflowID, 1), modelreg.WorkflowID(workflowID, 2)
	require.NoError(t, exec(t, source, `UPDATE take SET model_name = ? WHERE id = 'a-t'`, v1))
	require.NoError(t, exec(t, source, `INSERT INTO lock (id, project_id, target_type, target_id, parameter_name, mode, value) VALUES ('a-lv', 'a', 'shot', 'a-sh', 'video_model', 'lock', ?)`, `"`+v2+`"`))
	data, err := bundle.New(source).Export(ctx, "a")
	require.NoError(t, err)
	for _, tb := range data.Tables {
		assert.NotEqual(t, "comfy_workflow", tb.Name, "workflows are not bundled")
		assert.NotEqual(t, "comfy_workflow_version", tb.Name)
	}

	// This instance has no such workflow: both are named in the warning.
	empty := openTestDB(t)
	require.NoError(t, bundle.New(empty).Import(ctx, data))
	missing, err := bundle.New(empty).MissingWorkflows(ctx, "a")
	require.NoError(t, err)
	assert.Equal(t, []string{v1, v2}, missing)
	note := chatNote(t, empty)
	assert.Contains(t, note, v1)
	assert.Contains(t, note, v2)

	// This one has version 1 of it: only version 2 is missing.
	partial := openTestDB(t)
	require.NoError(t, exec(t, partial, `INSERT INTO comfy_workflow (id, name, kind, api_json, version) VALUES (?, 'W', 'video', '{}', 1)`, workflowID))
	require.NoError(t, exec(t, partial, `INSERT INTO comfy_workflow_version (workflow_id, version, api_json, assignment_json) VALUES (?, 1, '{}', '{}')`, workflowID))
	require.NoError(t, bundle.New(partial).Import(ctx, data))
	assert.NotContains(t, chatNote(t, partial), v1)
	assert.Contains(t, chatNote(t, partial), v2)

	// Nothing is missing, nothing is said: a Project of the built-in models gets no note.
	plain := openTestDB(t)
	seedDeepProject(t, plain, "b")
	plainData, err := bundle.New(plain).Export(ctx, "b")
	require.NoError(t, err)
	target := openTestDB(t)
	require.NoError(t, bundle.New(target).Import(ctx, plainData))
	assert.Equal(t, 0, countRows(t, target, "chat_message")["chat_message"])
}

func chatNote(t *testing.T, db *repository.DB) string {
	t.Helper()
	var content string
	require.NoError(t, db.Reader().QueryRow(`SELECT content FROM chat_message WHERE project_id = 'a' AND role = 'assistant'`).Scan(&content))
	return content
}
