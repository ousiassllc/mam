package editorstage_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/editor"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/editorstage"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/editortest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
)

func newFixture(t *testing.T) *editortest.Fixture {
	return editortest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		jobs.Register(r, env.Services)
		editor.Register(r, env.Services)
		editorstage.Register(r, env.Services)
	})
}

// toAudio makes the Rough Cut and judges it: the Project is in AUDIO. It returns the timeline id.
func toAudio(t *testing.T, f *editortest.Fixture) string {
	t.Helper()
	var v editor.ViewOutput
	f.CallOK("create_rough_cut", map[string]any{"project_id": f.ProjectID}, &v)
	id := v.Timeline.ID
	var ref editorstage.Ref
	f.CallOK("judge_edit_timeline", map[string]any{"timeline_id": id}, &ref)
	require.Equal(t, mcpkit.JobSucceeded, f.WaitJob(ref.JobID).State)
	require.Equal(t, "AUDIO", f.State())
	return id
}

// getJob asks the job the way an agent does, through the get_job tool.
func getJob(f *editortest.Fixture, id string) mcpkit.JobStatus {
	var st mcpkit.JobStatus
	f.CallOK("get_job", map[string]any{"job_id": id}, &st)
	return st
}

func TestEditingStages_FromJudgeToExportReportThroughGetJob(t *testing.T) {
	f := newFixture(t)
	f.WithScript()
	var rc editor.ViewOutput
	f.CallOK("create_rough_cut", map[string]any{"project_id": f.ProjectID}, &rc)
	timelineID := rc.Timeline.ID

	var judge editorstage.Ref
	f.CallOK("judge_edit_timeline", map[string]any{"timeline_id": timelineID}, &judge)
	assert.Equal(t, mcpkit.JobQueued, judge.State)
	assert.True(t, len(judge.JobID) > 0)
	st := f.WaitJob(judge.JobID)
	require.Equal(t, mcpkit.JobSucceeded, st.State, st.Error)
	assert.Equal(t, "pass", st.Result["verdict"])
	assert.Equal(t, "AUDIO", st.Result["project_state"])
	assert.Equal(t, timelineID, st.Target)
	assert.Equal(t, mcpkit.JobSucceeded, getJob(f, judge.JobID).State, "get_job answers again after the end")

	var audio editorstage.Ref
	f.CallOK("generate_audio", map[string]any{"project_id": f.ProjectID}, &audio)
	st = f.WaitJob(audio.JobID)
	require.Equal(t, mcpkit.JobSucceeded, st.State, st.Error)
	assert.EqualValues(t, 2, st.Result["speech_lines"])
	assert.NotEmpty(t, st.Result["audio_timeline_id"])

	var view editor.ViewOutput
	f.CallOK("run_final_qc", map[string]any{"project_id": f.ProjectID}, &view)
	require.NotNil(t, view.FinalQC)
	assert.Equal(t, "pass", view.FinalQC.Verdict)
	assert.Equal(t, "READY", view.State)
	require.NotNil(t, view.Audio)
	require.NotNil(t, view.Audio.Current)
	assert.True(t, *view.Audio.Current, "the audio was made for the latest edit")

	var export editorstage.Ref
	f.CallOK("export_video", map[string]any{"project_id": f.ProjectID, "aspect_ratio": "9:16", "resolution": "720p", "codec": "vp9"}, &export)
	st = f.WaitJob(export.JobID)
	require.Equal(t, mcpkit.JobSucceeded, st.State, st.Error)
	assert.Equal(t, "EXPORTED", st.Result["project_state"])
	assert.Equal(t, "vp9", st.Result["codec"])
	exportID, _ := st.Result["export_id"].(string)
	require.NotEmpty(t, exportID)

	var link mcpkit.Link
	f.CallOK("get_export_download_url", map[string]any{"project_id": f.ProjectID, "export_id": exportID}, &link)
	assert.Contains(t, link.FileName, exportID)
	assert.Contains(t, link.FileName, ".webm")
	rec := f.Download(link.URL)
	require.Equal(t, 200, rec.Code)
	assert.Equal(t, "video/webm", rec.Header().Get("Content-Type"))
	assert.NotZero(t, rec.Body.Len())

	f.CallOK("get_editor_view", map[string]any{"project_id": f.ProjectID}, &view)
	require.Len(t, view.Exports, 1)
	assert.Equal(t, exportID, view.Exports[0].ID)
}

func TestJudgeEditTimeline_RefusesAtOnceWhatTheScreenRefuses(t *testing.T) {
	f := newFixture(t)
	var v editor.ViewOutput
	f.CallOK("create_rough_cut", map[string]any{"project_id": f.ProjectID}, &v)

	assert.Contains(t, f.CallError("judge_edit_timeline", map[string]any{"timeline_id": "nope"}), "not_found")

	f.Exec(`UPDATE project SET state = 'READY' WHERE id = ?`, f.ProjectID)
	msg := f.CallError("judge_edit_timeline", map[string]any{"timeline_id": v.Timeline.ID})
	assert.Contains(t, msg, "conflict:", "only a Project in EDITING is judged")

	f.Exec(`UPDATE project SET state = 'EDITING' WHERE id = ?`, f.ProjectID)
	*f.Judge = "これは JSON ではありません"
	msg = f.CallError("judge_edit_timeline", map[string]any{"timeline_id": v.Timeline.ID})
	assert.Contains(t, msg, "upstream:", "the model gave no usable answer")
	assert.Equal(t, "EDITING", f.State(), "the Project is put back")
}

func TestJudgeEditTimeline_ReEditVerdictLeavesProjectEditingAndNeedsNoShot(t *testing.T) {
	f := newFixture(t)
	var v editor.ViewOutput
	f.CallOK("create_rough_cut", map[string]any{"project_id": f.ProjectID}, &v)
	*f.Judge = `{"verdict":"re_edit","findings":[{"clip_index":1,"issue":"テンポが遅い","action":"短くする"}]}`

	var ref editorstage.Ref
	f.CallOK("judge_edit_timeline", map[string]any{"timeline_id": v.Timeline.ID}, &ref)
	st := f.WaitJob(ref.JobID)
	require.Equal(t, mcpkit.JobSucceeded, st.State, st.Error)
	assert.Equal(t, "re_edit", st.Result["verdict"])
	assert.Equal(t, "EDITING", f.State())

	f.CallOK("get_editor_view", map[string]any{"project_id": f.ProjectID}, &v)
	require.NotNil(t, v.Timeline.Judge)
	require.Len(t, v.Timeline.Judge.Findings, 1)
	assert.Equal(t, "テンポが遅い", v.Timeline.Judge.Findings[0].Issue)

	msg := f.CallError("request_additional_shot", map[string]any{"timeline_id": v.Timeline.ID})
	assert.Contains(t, msg, "conflict:")
}

func TestGenerateAudio_OnlyInAudioAndExportOnlyWhenReady(t *testing.T) {
	f := newFixture(t)
	f.CallOK("create_rough_cut", map[string]any{"project_id": f.ProjectID}, nil)

	msg := f.CallError("generate_audio", map[string]any{"project_id": f.ProjectID})
	assert.Contains(t, msg, "conflict:")
	assert.Contains(t, msg, "EDITING")
	msg = f.CallError("export_video", map[string]any{"project_id": f.ProjectID})
	assert.Contains(t, msg, "conflict:")
	assert.Contains(t, f.CallError("generate_audio", map[string]any{"project_id": "nope"}), "not_found")
	assert.Contains(t, f.CallError("export_video", map[string]any{"project_id": "nope"}), "not_found")
}

func TestExportVideo_ChecksTheOptionsAndThatTheAudioIsOfTheLatestEdit(t *testing.T) {
	f := newFixture(t)
	f.WithScript()
	timelineID := toAudio(t, f)
	f.Exec(`UPDATE project SET state = 'READY' WHERE id = ?`, f.ProjectID)

	assert.Contains(t, f.CallError("export_video", map[string]any{"project_id": f.ProjectID, "codec": "prores"}), "validation:")
	assert.Contains(t, f.CallError("export_video", map[string]any{"project_id": f.ProjectID, "aspect_ratio": "3:2"}), "validation:")
	assert.Contains(t, f.CallError("export_video", map[string]any{"project_id": f.ProjectID, "resolution": "480p"}), "validation:")
	assert.Contains(t, f.CallError("export_video", map[string]any{"project_id": f.ProjectID}), "not_found", "no audio at all, as on the screen (404)")

	f.Exec(`UPDATE project SET state = 'AUDIO' WHERE id = ?`, f.ProjectID)
	var audio editorstage.Ref
	f.CallOK("generate_audio", map[string]any{"project_id": f.ProjectID}, &audio)
	require.Equal(t, mcpkit.JobSucceeded, f.WaitJob(audio.JobID).State)

	// The cut is edited again: the audio belongs to the version before.
	f.CallOK("save_edit_timeline", map[string]any{"timeline_id": timelineID, "clips": []map[string]any{{"shot_id": f.ShotID, "take_id": f.TakeID, "in_point": 0, "out_point": 2}}}, nil)
	f.Exec(`UPDATE project SET state = 'READY' WHERE id = ?`, f.ProjectID)
	msg := f.CallError("export_video", map[string]any{"project_id": f.ProjectID})
	assert.Contains(t, msg, "conflict:")
	assert.Contains(t, msg, "音声が最新の編集に対して作られていません")
	assert.Equal(t, "READY", f.State(), "a refused export changes nothing")
}

// A restart forgets the tracker, but the stage's end is in the job history: get_job answers the same.
func TestEditingStages_TheirJobsAreAnsweredAfterARestart(t *testing.T) {
	f := newFixture(t)
	f.WithScript()
	var rc editor.ViewOutput
	f.CallOK("create_rough_cut", map[string]any{"project_id": f.ProjectID}, &rc)
	var judge editorstage.Ref
	f.CallOK("judge_edit_timeline", map[string]any{"timeline_id": rc.Timeline.ID}, &judge)
	before := f.WaitJob(judge.JobID)
	require.Equal(t, mcpkit.JobSucceeded, before.State, before.Error)
	require.NotNil(t, before.EndedAt)
	var audio editorstage.Ref
	f.CallOK("generate_audio", map[string]any{"project_id": f.ProjectID}, &audio)
	audioBefore := f.WaitJob(audio.JobID)
	require.Equal(t, mcpkit.JobSucceeded, audioBefore.State, audioBefore.Error)

	after := f.Restart(func(env *mcptest.Env, r *mcpkit.Registrar) {
		jobs.Register(r, env.Services)
		editor.Register(r, env.Services)
		editorstage.Register(r, env.Services)
	})
	var got mcpkit.JobStatus
	after.CallOK("get_job", map[string]any{"job_id": judge.JobID}, &got)
	assert.Equal(t, before.State, got.State)
	assert.Equal(t, before.Kind, got.Kind)
	assert.Equal(t, before.Target, got.Target)
	assert.Equal(t, f.ProjectID, got.ProjectID, "the Project of the timeline")
	assert.True(t, before.StartedAt.Equal(got.StartedAt))
	assert.True(t, before.EndedAt.Equal(*got.EndedAt))
	assert.Equal(t, "pass", got.Result["verdict"], "the stage's result survived")
	after.CallOK("get_job", map[string]any{"job_id": audio.JobID}, &got)
	assert.Equal(t, mcpkit.JobSucceeded, got.State)
	assert.EqualValues(t, 2, got.Result["speech_lines"])
}
