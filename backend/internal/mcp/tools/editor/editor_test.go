package editor_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/editor"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/editortest"
)

func newFixture(t *testing.T) *editortest.Fixture {
	return editortest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) { editor.Register(r, env.Services) })
}

// clips builds the arguments of save_edit_timeline.
type clips []map[string]any

func (c clips) save(timelineID string) map[string]any {
	return map[string]any{"timeline_id": timelineID, "clips": []map[string]any(c)}
}

// oneClip is a clip of the Fixture's Shot and Take with the fields in extra added.
func oneClip(f *editortest.Fixture, in, out float64, extra map[string]any) clips {
	c := map[string]any{"shot_id": f.ShotID, "take_id": f.TakeID, "in_point": in, "out_point": out}
	for k, v := range extra {
		c[k] = v
	}
	return clips{c}
}

func roughCut(t *testing.T, f *editortest.Fixture) editor.ViewOutput {
	t.Helper()
	var v editor.ViewOutput
	f.CallOK("create_rough_cut", map[string]any{"project_id": f.ProjectID}, &v)
	return v
}

func TestGetEditorView_ShowsTheStateBeforeAndAfterTheRoughCut(t *testing.T) {
	f := newFixture(t)

	var v editor.ViewOutput
	f.CallOK("get_editor_view", map[string]any{"project_id": f.ProjectID}, &v)
	assert.Equal(t, "VISUAL_DESIGN", v.State)
	assert.Nil(t, v.Timeline, "no Rough Cut yet")
	assert.True(t, v.CanRender)
	assert.NotNil(t, v.Voices)
	assert.NotNil(t, v.Exports)
	assert.Equal(t, "16:9", v.ExportDefaults.AspectRatio)
	assert.Equal(t, "1080p", v.ExportDefaults.Resolution)
	assert.Contains(t, v.ExportChoices.Codecs, "vp9")

	rc := roughCut(t, f)
	require.NotNil(t, rc.Timeline)
	assert.Equal(t, "EDITING", rc.State)
	assert.Equal(t, 1, rc.Timeline.ClipCount)
	assert.InDelta(t, 4, rc.Timeline.DurationSeconds, 0.01)
	assert.True(t, rc.Timeline.Editable)

	f.CallOK("get_editor_view", map[string]any{"project_id": f.ProjectID}, &v)
	require.NotNil(t, v.Timeline)
	assert.Equal(t, rc.Timeline.ID, v.Timeline.ID)

	assert.Contains(t, f.CallError("get_editor_view", map[string]any{"project_id": "nope"}), "not_found")
}

func TestCreateRoughCut_RefusesWhatTheScreenRefuses(t *testing.T) {
	f := newFixture(t)

	f.Exec(`UPDATE shot SET selected_take_id = NULL WHERE id = ?`, f.ShotID)
	msg := f.CallError("create_rough_cut", map[string]any{"project_id": f.ProjectID})
	assert.Contains(t, msg, "validation:")
	assert.Contains(t, msg, "Selected Take")

	f.Exec(`UPDATE shot SET selected_take_id = ? WHERE id = ?`, f.TakeID, f.ShotID)
	f.Exec(`UPDATE project SET state = 'DRAFT' WHERE id = ?`, f.ProjectID)
	msg = f.CallError("create_rough_cut", map[string]any{"project_id": f.ProjectID})
	assert.Contains(t, msg, "conflict:")
	assert.Contains(t, msg, "DRAFT")

	assert.Contains(t, f.CallError("create_rough_cut", map[string]any{"project_id": "nope"}), "not_found")
}

func TestGetEditTimeline_ReturnsTheClipsTheEditorShows(t *testing.T) {
	f := newFixture(t)
	assert.Contains(t, f.CallError("get_edit_timeline", map[string]any{"project_id": f.ProjectID}), "not_found", "before the first Rough Cut")

	rc := roughCut(t, f)
	var tl editor.TimelineOutput
	f.CallOK("get_edit_timeline", map[string]any{"project_id": f.ProjectID}, &tl)

	assert.Equal(t, rc.Timeline.ID, tl.ID)
	assert.Equal(t, 1, tl.VersionNumber)
	require.Len(t, tl.Clips, 1)
	c := tl.Clips[0]
	assert.Equal(t, 1, c.Index)
	assert.Equal(t, f.ShotID, c.ShotID)
	assert.Equal(t, f.TakeID, c.TakeID)
	assert.Equal(t, "Scene 1 · Shot 1", c.Label)
	assert.Equal(t, "走り出す", c.Objective)
	assert.Equal(t, "main", c.Kind)
	assert.Equal(t, "cut", c.Transition.Type)
	assert.InDelta(t, 4, c.OutPoint, 0.01)
	assert.InDelta(t, 4, c.SourceSeconds, 0.3, "the Take file decides the source length")
	assert.InDelta(t, 0.5, tl.Limits.MinClipSeconds, 1e-9)
	assert.Equal(t, 60, tl.Limits.MaxCaptionRunes)
}

func TestSaveEditTimeline_SavesTheNextVersionAndRefusesWhatTheScreenRefuses(t *testing.T) {
	f := newFixture(t)
	rc := roughCut(t, f)
	id := rc.Timeline.ID

	var saved editor.TimelineOutput
	f.CallOK("save_edit_timeline", oneClip(f, 1, 3, map[string]any{"caption": "  走る  ", "transition": map[string]any{"type": "fade", "seconds": 0.5}}).save(id), &saved)
	assert.Equal(t, 2, saved.VersionNumber)
	assert.NotEqual(t, id, saved.ID)
	assert.InDelta(t, 2, saved.DurationSeconds, 0.01)
	require.Len(t, saved.Clips, 1)
	assert.Equal(t, "走る", saved.Clips[0].Caption, "the caption is cleaned as on the screen")
	assert.Equal(t, "cut", saved.Clips[0].Transition.Type, "the first clip has no transition")
	assert.InDelta(t, 1, saved.Clips[0].InPoint, 1e-9)

	// The version the editor was showing is no longer the latest: an old editor cannot overwrite it.
	msg := f.CallError("save_edit_timeline", oneClip(f, 0, 2, nil).save(id))
	assert.Contains(t, msg, "conflict:")

	latest := saved.ID
	msg = f.CallError("save_edit_timeline", oneClip(f, 1, 1.1, nil).save(latest))
	assert.Contains(t, msg, "validation:", "shorter than the minimum clip length")
	msg = f.CallError("save_edit_timeline", oneClip(f, 0, 90, nil).save(latest))
	assert.Contains(t, msg, "validation:", "the end point is past the Take")
	msg = f.CallError("save_edit_timeline", map[string]any{"timeline_id": latest, "clips": []map[string]any{}})
	assert.Contains(t, msg, "validation:", "a timeline needs a clip")
	msg = f.CallError("save_edit_timeline", oneClip(f, 0, 2, map[string]any{"take_id": "nope"}).save(latest))
	assert.Contains(t, msg, "validation:", "a Take that is not there")
	assert.Contains(t, f.CallError("save_edit_timeline", oneClip(f, 0, 2, nil).save("nope")), "not_found")

	var tl editor.TimelineOutput
	f.CallOK("get_edit_timeline", map[string]any{"project_id": f.ProjectID}, &tl)
	assert.Equal(t, saved.ID, tl.ID, "none of the refused saves made a version")
}

func TestSaveEditTimeline_RefusesATakeOfAnotherShot(t *testing.T) {
	f := newFixture(t)
	id := roughCut(t, f).Timeline.ID
	f.Exec(`INSERT INTO scene (id, project_id, order_index, title) VALUES ('s2', ?, 1, 'Other')`, f.ProjectID)
	f.Exec(`INSERT INTO shot (id, scene_id, order_index, objective, duration_seconds) VALUES ('sh2', 's2', 0, '別の Shot', 4)`)
	f.Exec(`INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot) VALUES ('pv2', 'sh2', 1, '{}')`)
	rel := f.ProjectID + "/take-1.mp4"
	f.Exec(`INSERT INTO take (id, prompt_version_id, model_name, model_parameters, file_path) VALUES ('take-other', 'pv2', 'kling-3.0', '{}', ?)`, rel)

	msg := f.CallError("save_edit_timeline", oneClip(f, 0, 2, map[string]any{"take_id": "take-other"}).save(id))
	assert.Contains(t, msg, "validation:")
	assert.Contains(t, msg, "Take ではありません")
}

func TestRequestAdditionalShot_NeedsAJudgedVersion(t *testing.T) {
	f := newFixture(t)
	id := roughCut(t, f).Timeline.ID

	msg := f.CallError("request_additional_shot", map[string]any{"timeline_id": id})
	assert.Contains(t, msg, "conflict:")
	assert.Contains(t, msg, "Editor Judge")
	assert.Contains(t, f.CallError("request_additional_shot", map[string]any{"timeline_id": "nope"}), "not_found")
}

func TestRunFinalQC_OnlyAfterAudio(t *testing.T) {
	f := newFixture(t)
	roughCut(t, f)

	msg := f.CallError("run_final_qc", map[string]any{"project_id": f.ProjectID})
	assert.Contains(t, msg, "conflict:")
	assert.Contains(t, msg, "EDITING")
	assert.Contains(t, f.CallError("run_final_qc", map[string]any{"project_id": "nope"}), "not_found")
}
