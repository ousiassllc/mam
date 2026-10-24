package takes_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/shotgen"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/shottest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/takes"
)

func newFixture(t *testing.T, o shottest.Options) *shottest.Fixture {
	t.Helper()
	return shottest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		jobs.Register(r, env.Services)
		shotgen.Register(r, env.Services)
		takes.Register(r, env.Services)
	}, o)
}

func get(t *testing.T, f *shottest.Fixture, url string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	f.Registrar.Files.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	return rec
}

func TestListShotTakes_ShowsScoresStatusAndWhichActionsAreOpen(t *testing.T) {
	f := newFixture(t, shottest.Options{Verdict: shottest.FailVerdict})
	assert.Contains(t, f.CallError("list_shot_takes", map[string]any{"shot_id": "missing"}), "not_found")

	var empty takes.ListTakesOutput
	f.CallOK("list_shot_takes", map[string]any{"shot_id": f.ShotID}, &empty)
	assert.Empty(t, empty.Takes)
	assert.NotNil(t, empty.Takes, "an empty list, not null")

	ids := f.Generate(t)
	var judged shotgen.TakeJobOutput
	f.CallOK("judge_take", map[string]any{"take_id": ids[0]}, &judged)
	require.Eventually(t, func() bool {
		var st mcpkit.JobStatus
		f.CallOK("get_job", map[string]any{"job_id": judged.JobID}, &st)
		return st.State.Final()
	}, 30e9, 10e6)
	f.WaitIdle(t)

	var out takes.ListTakesOutput
	f.CallOK("list_shot_takes", map[string]any{"shot_id": f.ShotID}, &out)
	require.Len(t, out.Takes, len(ids))
	assert.Equal(t, "A", out.Takes[0].Label)
	assert.Equal(t, ids[0], out.Takes[0].ID)
	assert.Equal(t, "fail", out.Takes[0].Verdict)
	for _, tk := range out.Takes {
		assert.NotNil(t, tk.Scores, "every generated Take is judged")
	}
	assert.True(t, out.Takes[0].Selectable)
	assert.Equal(t, domain.MaxTakeSegments, out.Limits.MaxSegments)
	assert.NotZero(t, out.Limits.MinAxisScore)
}

func TestSelectAndRejectTake_FollowTheStateMachineOfTheScreen(t *testing.T) {
	f := newFixture(t, shottest.Options{})
	ids := f.Generate(t)
	require.GreaterOrEqual(t, len(ids), 2)
	assert.Contains(t, f.CallError("select_take", map[string]any{"take_id": "missing"}), "not_found")
	assert.Contains(t, f.CallError("reject_take", map[string]any{"take_id": "missing"}), "not_found")

	var out takes.ShotOutput
	f.CallOK("select_take", map[string]any{"take_id": ids[1]}, &out)
	assert.Equal(t, ids[1], out.Shot.SelectedTakeID)
	assert.Contains(t, f.CallError("select_take", map[string]any{"take_id": ids[1]}), "validation", "already selected")

	f.CallOK("reject_take", map[string]any{"take_id": ids[0]}, &out)
	for _, tk := range out.Shot.Takes {
		if tk.ID == ids[0] {
			assert.Equal(t, "rejected", tk.Status)
		}
	}
	assert.Contains(t, f.CallError("reject_take", map[string]any{"take_id": ids[0]}), "validation", "already rejected")
	assert.Contains(t, f.CallError("select_take", map[string]any{"take_id": ids[0]}), "validation", "a rejected Take cannot be selected")
	assert.Equal(t, 1, f.Count(t, `SELECT COUNT(*) FROM take WHERE id = ?`, ids[0]), "rejecting keeps the Take")
}

func TestTakeDownloads_VideoAndThumbnailAreServedByTicketAndArchivedTakesAreNotFound(t *testing.T) {
	f := newFixture(t, shottest.Options{FFmpeg: true})
	ids := f.Generate(t)

	var video takes.DownloadOutput
	f.CallOK("get_take_video_url", map[string]any{"take_id": ids[0]}, &video)
	rec := get(t, f, video.URL)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "video/mp4", rec.Header().Get("Content-Type"))
	body, _ := io.ReadAll(rec.Body)
	assert.NotEmpty(t, body)
	assert.Equal(t, ids[0]+".mp4", video.FileName)

	var thumb takes.DownloadOutput
	f.CallOK("get_take_thumbnail_url", map[string]any{"take_id": ids[0]}, &thumb)
	rec = get(t, f, thumb.URL)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "image/jpeg", rec.Header().Get("Content-Type"))

	assert.Contains(t, f.CallError("get_take_video_url", map[string]any{"take_id": "missing"}), "not_found")
	f.Exec(t, `UPDATE take SET file_path = '' WHERE id = ?`, ids[0])
	assert.Contains(t, f.CallError("get_take_video_url", map[string]any{"take_id": ids[0]}), "not_found", "the file was archived")
}

func TestTakeThumbnail_WithoutFFmpegIsUnavailable(t *testing.T) {
	f := newFixture(t, shottest.Options{})
	ids := f.Generate(t)
	assert.Contains(t, f.CallError("get_take_thumbnail_url", map[string]any{"take_id": ids[0]}), "unavailable")
}

func TestUsePartialTakes_JoinsSegmentsAndRejectsBadRanges(t *testing.T) {
	f := newFixture(t, shottest.Options{FFmpeg: true})
	ids := f.Generate(t)
	require.GreaterOrEqual(t, len(ids), 2)

	assert.Contains(t, f.CallError("use_partial_takes", map[string]any{"shot_id": f.ShotID}), "validation", "neither segments nor words")
	assert.Contains(t, f.CallError("use_partial_takes", map[string]any{"shot_id": f.ShotID, "segments": []map[string]any{{"take_id": ids[0], "start": 0, "end": 99}}}), "validation", "beyond the Take")
	assert.Contains(t, f.CallError("use_partial_takes", map[string]any{"shot_id": f.ShotID, "instruction": "Take Z の 0-2秒"}), "validation")
	assert.Contains(t, f.CallError("use_partial_takes", map[string]any{"shot_id": "missing", "instruction": "Take A の 0-2秒"}), "not_found")

	before := f.Count(t, `SELECT COUNT(*) FROM take`)
	var out takes.ShotOutput
	f.CallOK("use_partial_takes", map[string]any{"shot_id": f.ShotID, "segments": []map[string]any{
		{"take_id": ids[0], "start": 0, "end": 2}, {"take_id": ids[1], "start": 2, "end": 4},
	}}, &out)
	assert.Equal(t, before+1, f.Count(t, `SELECT COUNT(*) FROM take`), "one new Take")
	assert.NotEmpty(t, out.Shot.SelectedTakeID)
	assert.NotContains(t, ids, out.Shot.SelectedTakeID, "the joined Take is the Shot's Take")

	var words takes.ShotOutput
	f.CallOK("use_partial_takes", map[string]any{"shot_id": f.ShotID, "instruction": "Take A の 0-2秒 + Take B の 2-4秒"}, &words)
	assert.Equal(t, before+2, f.Count(t, `SELECT COUNT(*) FROM take`))
}

func TestUsePartialTakes_WithoutFFmpegIsUnavailable(t *testing.T) {
	f := newFixture(t, shottest.Options{})
	f.Generate(t)
	assert.Contains(t, f.CallError("use_partial_takes", map[string]any{"shot_id": f.ShotID, "instruction": "Take A の 0-2秒"}), "unavailable")
}

func TestGetTakeTrace_ReturnsSpecModelJudgeAndRepairHistory(t *testing.T) {
	f := newFixture(t, shottest.Options{Verdict: shottest.FailVerdict})
	assert.Contains(t, f.CallError("get_take_trace", map[string]any{"take_id": "missing"}), "not_found")
	ids := f.Generate(t)

	var judged shotgen.TakeJobOutput
	f.CallOK("judge_take", map[string]any{"take_id": ids[0]}, &judged)
	require.Eventually(t, func() bool {
		var st mcpkit.JobStatus
		f.CallOK("get_job", map[string]any{"job_id": judged.JobID}, &st)
		return st.State.Final()
	}, 30e9, 10e6)
	f.WaitIdle(t)

	var tr takes.TraceOutput
	f.CallOK("get_take_trace", map[string]any{"take_id": ids[0]}, &tr)
	assert.Equal(t, f.ProjectID, tr.ProjectID)
	assert.Equal(t, ids[0], tr.Take.ID)
	assert.NotNil(t, tr.PromptVersion.SpecSnapshot, "the Source Shot Spec")
	assert.NotNil(t, tr.Output)
	require.NotEmpty(t, tr.Judgements)
	assert.Equal(t, "fail", string(tr.Judgements[0].Result))
	assert.NotNil(t, tr.Repairs, "an empty list, not null")
}
