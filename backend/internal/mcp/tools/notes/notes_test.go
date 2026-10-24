package notes_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/notes"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/series"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/worldtest"
)

func newEnv(t *testing.T) (*mcptest.Env, worldtest.Project) {
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		notes.Register(r, env.Services)
		series.Register(r, env.Services)
	})
	return env, worldtest.SeedProject(t, env)
}

func TestProjectNotes_CreateListUpdateArchiveRestore(t *testing.T) {
	env, p := newEnv(t)
	var list notes.List
	env.CallOK("list_project_notes", map[string]any{"project_id": p.ID}, &list)
	assert.NotNil(t, list.Notes)
	assert.Empty(t, list.Notes)

	var n notes.Note
	env.CallOK("create_project_note", map[string]any{"project_id": p.ID, "content": "主人公は左利き\n詳細は後で", "category": "character", "pinned": true}, &n)
	assert.Equal(t, p.ID, n.ProjectID)
	assert.Equal(t, "主人公は左利き", n.Title, "the first line of the text, as the screen does")
	assert.Equal(t, "character", n.Category)
	assert.True(t, n.Pinned)
	assert.Equal(t, 1, n.VersionNumber)
	assert.Equal(t, "manual", n.Source)
	assert.False(t, n.Archived)

	var second notes.Note
	env.CallOK("create_project_note", map[string]any{"project_id": p.ID, "title": "トーン", "content": "落ち着いた口調"}, &second)
	assert.Equal(t, "other", second.Category, "without a Reasoning LLM the note is 'other'")
	assert.NotEqual(t, n.ID, second.ID, "the answer is the note that was made, not another one of the list")

	env.CallOK("list_project_notes", map[string]any{"project_id": p.ID}, &list)
	require.Len(t, list.Notes, 2)

	var updated notes.Note
	env.CallOK("update_note", map[string]any{"note_id": n.ID, "content": "主人公は右利き"}, &updated)
	assert.Equal(t, 2, updated.VersionNumber, "a new text is a new version")
	assert.Equal(t, "右利き", strings.TrimPrefix(updated.Content, "主人公は"))
	require.Len(t, updated.Versions, 2)

	env.CallOK("update_note", map[string]any{"note_id": n.ID, "pinned": false, "category": "world"}, &updated)
	assert.Equal(t, 2, updated.VersionNumber, "category and pinned do not make a version")
	assert.False(t, updated.Pinned)
	assert.Equal(t, "world", updated.Category)

	var archived notes.Note
	env.CallOK("archive_note", map[string]any{"note_id": n.ID}, &archived)
	assert.True(t, archived.Archived)
	assert.NotNil(t, archived.ArchivedAt)
	env.CallOK("list_project_notes", map[string]any{"project_id": p.ID}, &list)
	assert.Len(t, list.Notes, 2, "an archived note stays listed")
	env.CallOK("update_note", map[string]any{"note_id": n.ID, "archived": false}, &archived)
	assert.False(t, archived.Archived, "archived=false brings it back")

	var restored notes.Note
	env.CallOK("restore_note_version", map[string]any{"note_id": n.ID, "version_number": 1}, &restored)
	assert.Equal(t, 3, restored.VersionNumber, "a restore appends a version")
	assert.Equal(t, "主人公は左利き\n詳細は後で", restored.Content)
}

func TestNotes_RefuseWhatTheScreenRefuses(t *testing.T) {
	env, p := newEnv(t)
	assert.Contains(t, env.CallError("create_project_note", map[string]any{"project_id": p.ID, "content": "  "}), "validation:")
	assert.Contains(t, env.CallError("create_project_note", map[string]any{"project_id": p.ID, "content": strings.Repeat("あ", 4001)}), "validation:")
	assert.Contains(t, env.CallError("create_project_note", map[string]any{"project_id": p.ID, "title": strings.Repeat("あ", 101), "content": "x"}), "validation:")
	assert.Contains(t, env.CallError("create_project_note", map[string]any{"project_id": p.ID, "content": "x", "category": "poetry"}), "validation:")
	assert.Contains(t, env.CallError("create_project_note", map[string]any{"project_id": "nope", "content": "x"}), "not_found")
	assert.Contains(t, env.CallError("list_project_notes", map[string]any{"project_id": "nope"}), "not_found")
	assert.Contains(t, env.CallError("create_series_note", map[string]any{"series_id": "nope", "content": "x"}), "not_found")
	assert.Contains(t, env.CallError("list_series_notes", map[string]any{"series_id": "nope"}), "not_found")

	var n notes.Note
	env.CallOK("create_project_note", map[string]any{"project_id": p.ID, "content": "x"}, &n)
	assert.Contains(t, env.CallError("update_note", map[string]any{"note_id": n.ID, "content": ""}), "validation:", "a note cannot be emptied")
	assert.Contains(t, env.CallError("update_note", map[string]any{"note_id": n.ID, "title": ""}), "validation:")
	assert.Contains(t, env.CallError("update_note", map[string]any{"note_id": n.ID, "category": "poetry"}), "validation:")
	assert.Contains(t, env.CallError("update_note", map[string]any{"note_id": "nope", "pinned": true}), "not_found")
	assert.Contains(t, env.CallError("archive_note", map[string]any{"note_id": "nope"}), "not_found")
	assert.Contains(t, env.CallError("restore_note_version", map[string]any{"note_id": n.ID, "version_number": 9}), "not_found")
	assert.Contains(t, env.CallError("restore_note_version", map[string]any{"note_id": n.ID, "version_number": 0}), "validation:")
	assert.Contains(t, env.CallError("restore_note_version", map[string]any{"note_id": "nope", "version_number": 1}), "not_found")
}

func TestSeriesNotes_AreCanonAndCopiedIntoNewEpisodesButNotArchivedOnes(t *testing.T) {
	env, _ := newEnv(t)
	var s series.CreateOutput
	env.CallOK("create_series", map[string]any{"name": "Mio Diaries"}, &s)

	var kept, dropped notes.Note
	env.CallOK("create_series_note", map[string]any{"series_id": s.SeriesID, "content": "世界観は近未来の東京", "category": "world"}, &kept)
	env.CallOK("create_series_note", map[string]any{"series_id": s.SeriesID, "content": "没案", "category": "other"}, &dropped)
	assert.Equal(t, s.SeriesID, kept.SeriesID)
	assert.Empty(t, kept.ProjectID)
	env.CallOK("archive_note", map[string]any{"note_id": dropped.ID}, &dropped)
	assert.True(t, dropped.Archived)

	var list notes.List
	env.CallOK("list_series_notes", map[string]any{"series_id": s.SeriesID}, &list)
	assert.Equal(t, s.SeriesID, list.SeriesID)
	assert.Len(t, list.Notes, 2)

	var ep series.CreateEpisodeOutput
	env.CallOK("create_episode", map[string]any{"series_id": s.SeriesID}, &ep)
	env.CallOK("list_project_notes", map[string]any{"project_id": ep.ProjectID}, &list)
	require.Len(t, list.Notes, 1, "the archived canon note is not copied")
	assert.Equal(t, "世界観は近未来の東京", list.Notes[0].Content)
	assert.Equal(t, kept.ID, list.Notes[0].OriginID)

	env.CallOK("update_note", map[string]any{"note_id": kept.ID, "content": "世界観は近未来の大阪"}, &kept)
	env.CallOK("list_project_notes", map[string]any{"project_id": ep.ProjectID}, &list)
	assert.Equal(t, "世界観は近未来の東京", list.Notes[0].Content, "changing the canon does not reach an existing Episode")
}
