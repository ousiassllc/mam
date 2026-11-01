package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

// mp4Video is a video model that makes a real 4 s test video, so that ffmpeg can splice it.
type mp4Video struct{ data []byte }

func newMP4Video(t *testing.T) *mp4Video {
	t.Helper()
	return &mp4Video{data: testenv.PatternVideo(t, "320x240", 4)}
}

func (v *mp4Video) GenerateVideo(context.Context, string, modeladapter.VideoRequest) (modeladapter.VideoResult, error) {
	return modeladapter.VideoResult{File: modeladapter.BlobOf(v.data), MimeType: "video/mp4"}, nil
}

// Judge answers: one camera Issue at 1.5-2.5 s, or the same Issue about the whole Take.
const (
	rangedVerdict = `{"result":"fail","overall_confidence":0.8,"issues":[{"type":"camera_speed","severity":"medium","dimension":"camera_motion","time_range":"1.5-2.5s"}],
		"preserve":["character"],"repair_strategy":{"action":"partial_regeneration","instructions":["reduce_camera_speed_40_percent"]}}`
	wholeVerdict = `{"result":"fail","overall_confidence":0.8,"issues":[{"type":"camera_speed","severity":"medium","dimension":"camera_motion"}],
		"preserve":["character"],"repair_strategy":{"action":"partial_regeneration","instructions":["reduce_camera_speed_40_percent"]}}`
)

// newPartialApp is a DIRECTOR MODE app with ffmpeg whose video model makes real videos.
func newPartialApp(t *testing.T, verdict, command string) *judgeApp {
	t.Helper()
	tools, err := media.Look()
	if err != nil {
		testenv.MissingFFmpeg(t, "ffmpeg / ffprobe are not installed")
	}
	j := newJudgeAppOpts(t, judgeOpts{verdict: verdict, video: newMP4Video(t), media: &tools, command: command}, 0.95, 0.95, 0.95)
	j.director()
	return j
}

type spliceRow struct {
	ID          string
	Status      string
	Source      string
	Regenerated string
	Segments    []domain.TakeSegment
}

// splices are the Takes made by splicing a Partial Regeneration.
func (j *judgeApp) splices() []spliceRow {
	j.t.Helper()
	rows, err := j.db.Reader().Query(`SELECT id, status, model_parameters FROM take WHERE json_extract(model_parameters, '$.kind') = ?`, domain.PartialTakeKind)
	require.NoError(j.t, err)
	defer func() { _ = rows.Close() }()
	var out []spliceRow
	for rows.Next() {
		var r spliceRow
		var params string
		require.NoError(j.t, rows.Scan(&r.ID, &r.Status, &params))
		var p struct {
			Source            string               `json:"source"`
			RegeneratedTakeID string               `json:"regenerated_take_id"`
			Segments          []domain.TakeSegment `json:"segments"`
		}
		require.NoError(j.t, json.Unmarshal([]byte(params), &p))
		r.Source, r.Regenerated, r.Segments = p.Source, p.RegeneratedTakeID, p.Segments
		out = append(out, r)
	}
	return out
}

func (j *judgeApp) selectedTake() string {
	var id *string
	require.NoError(j.t, j.db.Reader().QueryRow(`SELECT selected_take_id FROM shot WHERE id = ?`, j.shotID).Scan(&id))
	if id == nil {
		return ""
	}
	return *id
}

func TestPartialRegeneration_RepairSplicesTheRegeneratedRangeIntoTheFailedTakeAsANewTake(t *testing.T) {
	j := newPartialApp(t, rangedVerdict, "")
	ids := j.generateAndJudge()
	source := ids[0]
	selected := j.selectedTake()

	rec := j.form(http.MethodPost, "/takes/"+source+"/repair", nil)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	j.waitTakes(5) // the regenerated Take of the whole Shot, and the spliced one

	splices := j.splices()
	require.Len(t, splices, 1)
	sp := splices[0]
	assert.Equal(t, "partial_regeneration", sp.Source)
	assert.Equal(t, "candidate", sp.Status, "the AI's proposal does not replace the Shot's Take")
	assert.Equal(t, selected, j.selectedTake())
	assert.NotContains(t, ids, sp.ID)
	assert.NotContains(t, ids, sp.Regenerated, "the regenerated Take is a new row too")

	// The failed Take outside 1.3-2.7 s (the range with its margin), the regenerated one inside.
	require.Len(t, sp.Segments, 3)
	assert.Equal(t, source, sp.Segments[0].TakeID)
	assert.Equal(t, sp.Regenerated, sp.Segments[1].TakeID)
	assert.Equal(t, source, sp.Segments[2].TakeID)
	assert.InDelta(t, 1.3, sp.Segments[1].Start, 1e-6)
	assert.InDelta(t, 2.7, sp.Segments[2].Start, 1e-6)

	var resulting string
	require.NoError(t, j.db.Reader().QueryRow(`SELECT resulting_take_id FROM repair_action`).Scan(&resulting))
	assert.Equal(t, sp.Regenerated, resulting, "the repair traces the regenerated Take")

	require.EventuallyWithT(t, func(c *assert.CollectT) { assert.Equal(c, 1, j.results(c, sp.ID)) }, 30*time.Second, 10*time.Millisecond, "the spliced Take is judged")
	for _, id := range ids {
		assert.Equal(t, 1, j.count(`SELECT COUNT(*) FROM take WHERE id = ?`, id), "the Takes that existed are still there")
	}
	assert.Equal(t, 5, j.count(`SELECT COUNT(*) FROM take`))
}

func TestPartialRegeneration_AnIssueOfTheWholeTakeIsNotSpliced(t *testing.T) {
	j := newPartialApp(t, wholeVerdict, "")
	ids := j.generateAndJudge()

	require.Equal(t, http.StatusAccepted, j.form(http.MethodPost, "/takes/"+ids[0]+"/repair", nil).Code)
	j.waitTakes(4)

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Equal(c, 1, j.countOn(c, `SELECT COUNT(*) FROM repair_action WHERE resulting_take_id IS NOT NULL`))
	}, 30*time.Second, 10*time.Millisecond)
	assert.Empty(t, j.splices(), "only a Judge Issue with a time range can be fixed by regenerating that range")
}

const partialCommand = `{"scene":1,"shot":1,"operations":[{"type":"partial_regenerate","time_ranges":["1.0-2.0"],"changes":["camera"],"preserve":[],"instruction":"camera movement: static"}]}`

func TestPartialRegeneration_DirectorCommandFixesTheNamedRangeAndKeepsTheRestOfTheTake(t *testing.T) {
	j := newPartialApp(t, wholeVerdict, partialCommand)
	ids := j.generateAndJudge()

	rec := j.form(http.MethodPost, "/shots/"+j.shotID+"/director-command", url.Values{"content": {"1〜2秒目のカメラの揺れだけ直して"}})
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	j.waitTakes(5)

	splices := j.splices()
	require.Len(t, splices, 1)
	sp := splices[0]
	assert.Contains(t, ids, sp.Segments[0].TakeID, "the rest is a Take that existed before")
	assert.Equal(t, sp.Segments[0].TakeID, sp.Segments[len(sp.Segments)-1].TakeID)
	assert.Equal(t, sp.Regenerated, sp.Segments[1].TakeID)
	assert.NotContains(t, ids, sp.Regenerated)
	assert.Equal(t, 0, j.count(`SELECT COUNT(*) FROM repair_action`), "the director asked: no Retry Policy step is recorded")
	assert.Contains(t, j.get("/projects/"+j.project, false).Body.String(), "1.0-2.0秒")
	require.EventuallyWithT(t, func(c *assert.CollectT) { assert.Equal(c, 1, j.results(c, sp.ID)) }, 30*time.Second, 10*time.Millisecond)
}

func TestPartialRegeneration_DirectorCommandWithoutFfmpegSaysSoAndMakesNothing(t *testing.T) {
	j := newJudgeAppOpts(t, judgeOpts{verdict: wholeVerdict, command: partialCommand}, 0.95, 0.95, 0.95)
	j.director()
	j.generateAndJudge()

	rec := j.form(http.MethodPost, "/shots/"+j.shotID+"/director-command", url.Values{"content": {"1〜2秒目だけ直して"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, 3, j.count(`SELECT COUNT(*) FROM take`))
	assert.Contains(t, j.get("/projects/"+j.project, false).Body.String(), "ffmpeg")
}

func TestPartialRegeneration_DirectorCommandBeyondTheShotLengthMakesNothingAndSaysTheLength(t *testing.T) {
	const beyond = `{"scene":1,"shot":1,"operations":[{"type":"partial_regenerate","time_ranges":["50.0-60.0"],"changes":["camera"],"preserve":[],"instruction":"camera movement: static"}]}`
	j := newPartialApp(t, wholeVerdict, beyond)
	j.generateAndJudge()
	takes, jobs := j.count(`SELECT COUNT(*) FROM take`), j.count(`SELECT COUNT(*) FROM job`)

	rec := j.form(http.MethodPost, "/shots/"+j.shotID+"/director-command", url.Values{"content": {"50〜60秒目だけ直して"}})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, takes, j.count(`SELECT COUNT(*) FROM take`), "no Take is paid for")
	assert.Equal(t, jobs, j.count(`SELECT COUNT(*) FROM job`), "no job is queued")
	assert.Contains(t, j.get("/projects/"+j.project, false).Body.String(), "4.0 秒の Shot")
}
