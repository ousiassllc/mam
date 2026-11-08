package bundle_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/repository/bundle"
)

// Choosing AUTO is the consent to the paid generation of the pipeline on the instance where it was chosen (#1115), so a bundle does not carry it: an
// imported AUTO Project is DIRECTOR and says so in its chat, whatever else it holds; every other column is as the bundle has it. A DIRECTOR Project
// comes back as it was, with no note.
func TestBundleRepository_ImportTurnsAutoIntoDirectorAndSaysSo(t *testing.T) {
	ctx := context.Background()
	source := openTestDB(t)
	seedDeepProject(t, source, "a")
	require.NoError(t, exec(t, source, `UPDATE project SET mode = 'auto', state = 'VISUAL_DESIGN', confirmation_level = 'key_check', project_budget_ceiling = 12.5 WHERE id = 'a'`))
	data, err := bundle.New(source).Export(ctx, "a")
	require.NoError(t, err)

	target := openTestDB(t)
	require.NoError(t, bundle.New(target).Import(ctx, data))

	var mode, state, level string
	var ceiling float64
	require.NoError(t, target.Reader().QueryRow(`SELECT mode, state, confirmation_level, project_budget_ceiling FROM project WHERE id = 'a'`).Scan(&mode, &state, &level, &ceiling))
	assert.Equal(t, "director", mode, "the AUTO of the bundle is not this instance's consent")
	assert.Equal(t, "VISUAL_DESIGN", state, "the other columns are as the bundle has them")
	assert.Equal(t, "key_check", level)
	assert.Equal(t, 12.5, ceiling)
	var note string
	require.NoError(t, target.Reader().QueryRow(`SELECT content FROM chat_message WHERE project_id = 'a' AND role = 'assistant'`).Scan(&note))
	assert.Equal(t, bundle.ImportedAutoNotice, note)

	// A Project that was DIRECTOR needs no note.
	require.NoError(t, exec(t, source, `UPDATE project SET mode = 'director' WHERE id = 'a'`))
	data, err = bundle.New(source).Export(ctx, "a")
	require.NoError(t, err)
	plain := openTestDB(t)
	require.NoError(t, bundle.New(plain).Import(ctx, data))
	assert.Equal(t, 0, countRows(t, plain, "chat_message")["chat_message"])
}
