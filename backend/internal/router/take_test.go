package router_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

type takeApp struct {
	*app
	dir     string
	project string
	shotID  string
	tools   media.Tools
}

// newTakeApp is an app with ffmpeg and a Shot with three Takes: t1 and t2 are 1080p, 4 s; t3 is a
// 4K (2160p) Take of 1 s. Their files are real videos.
func newTakeApp(t *testing.T) *takeApp { return newTakeAppWith(t, nil) }

// newTakeAppWith is newTakeApp with the chance to change the service dependencies.
func newTakeAppWith(t *testing.T, mutate func(*service.Deps)) *takeApp {
	t.Helper()
	tools, err := media.Look()
	if err != nil {
		testenv.MissingFFmpeg(t, "ffmpeg / ffprobe are not installed")
	}
	keyring.MockInit()
	dir := t.TempDir()
	db, err := repository.Open(context.Background(), filepath.Join(dir, "mam.db"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	deps := service.Deps{DB: db, Credentials: credential.NewKeyring(), AssetsDir: filepath.Join(dir, "assets"), Media: &tools}
	if mutate != nil {
		mutate(&deps)
	}
	svc := service.New(deps)
	a := &app{t: t, db: db, engine: router.New(svc, router.Options{}), svc: svc}
	a.cookie, a.token = bootstrap(t, a.engine)
	ta := &takeApp{app: a, dir: dir, tools: tools}
	ta.project = a.createProject("美容液のCMを作りたい")
	rec := a.form(http.MethodPost, "/projects/"+ta.project+"/scenes", url.Values{"title": {"Street"}})
	m := sceneIDRe.FindStringSubmatch(rec.Body.String())
	require.NotNil(t, m)
	rec = a.form(http.MethodPost, "/scenes/"+m[1]+"/shots", url.Values{"objective": {"走り出す"}, "duration_seconds": {"4"}})
	require.Equal(t, http.StatusCreated, rec.Code)
	m = shotIDRe.FindStringSubmatch(rec.Body.String())
	require.NotNil(t, m)
	ta.shotID = m[1]

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "assets", ta.project), 0o700))
	ta.video("t1", "1920x1080", 4)
	ta.video("t2", "1920x1080", 4)
	ta.video("t3", "3840x2160", 1)
	require.NoError(t, db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot) VALUES ('pv1', ?, 1, '{}')`, ta.shotID); err != nil {
			return err
		}
		for i, spec := range []struct{ id, res string }{{"t1", "1080p"}, {"t2", "1080p"}, {"t3", "2160p"}} {
			rel := ta.project + "/" + spec.id + ".mp4"
			if _, err := tx.ExecContext(ctx, `INSERT INTO asset (id, project_id, type, file_path, mime_type) VALUES ('a-'||?, ?, 'video', ?, 'video/mp4')`, spec.id, ta.project, rel); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO take (id, prompt_version_id, model_name, model_parameters, file_path, created_at) VALUES (?, 'pv1', 'kling-3.0', ?, ?, ?)`,
				spec.id, `{"resolution":"`+spec.res+`","duration_seconds":`+map[string]string{"t1": "4", "t2": "4", "t3": "1"}[spec.id]+`}`, rel, "2026-01-01T00:00:0"+strconv.Itoa(i)+".000Z"); err != nil {
				return err
			}
		}
		return nil
	}))
	return ta
}

// video writes a test video of the Take (a colour test pattern with a tone).
func (ta *takeApp) video(name, size string, seconds int) {
	ta.t.Helper()
	path := filepath.Join(ta.dir, "assets", ta.project, name+".mp4")
	d := strconv.Itoa(seconds)
	out, err := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=size="+size+":rate=10:duration="+d,
		"-f", "lavfi", "-i", "sine=frequency=440:duration="+d, "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", path).CombinedOutput()
	require.NoError(ta.t, err, string(out))
}

type takeListBody struct {
	ShotID     string `json:"shot_id"`
	PartialURL string `json:"partial_url"`
	Takes      []struct {
		ID           string  `json:"id"`
		Label        string  `json:"label"`
		Status       string  `json:"status"`
		Adopted      bool    `json:"adopted"`
		Resolution   string  `json:"resolution"`
		FPS          float64 `json:"fps"`
		Duration     float64 `json:"duration_seconds"`
		Proxied      bool    `json:"proxied"`
		VideoURL     string  `json:"video_url"`
		ThumbnailURL string  `json:"thumbnail_url"`
		SelectURL    string  `json:"select_url"`
		RejectURL    string  `json:"reject_url"`
		Scores       *struct {
			Product, Acting, Camera, Continuity, Overall float64
		} `json:"scores"`
	} `json:"takes"`
}

func (ta *takeApp) takes() takeListBody {
	ta.t.Helper()
	rec := ta.get("/api/v1/shots/"+ta.shotID+"/takes", false)
	require.Equal(ta.t, http.StatusOK, rec.Code, rec.Body.String())
	var b takeListBody
	require.NoError(ta.t, json.Unmarshal(rec.Body.Bytes(), &b))
	return b
}

func (ta *takeApp) statusOf(id string) string {
	ta.t.Helper()
	for _, tk := range ta.takes().Takes {
		if tk.ID == id {
			return tk.Status
		}
	}
	ta.t.Fatalf("no take %s", id)
	return ""
}

func TestTakeAPI_ListsTakesOldestFirstWithLabelsLinksAndScores(t *testing.T) {
	ta := newTakeApp(t)
	_, err := repository.NewJudgeRepository(ta.db).Save(context.Background(), domain.JudgeResult{
		TakeID: "t1", Result: domain.JudgeFail,
		Issues: []domain.JudgeIssue{{Type: "product_logo_deformation", Severity: domain.SeverityHigh, Dimension: domain.DimLogoTextAccuracy, TimeRange: "2.1-2.8s"}},
	})
	require.NoError(t, err)

	b := ta.takes()
	assert.Equal(t, ta.shotID, b.ShotID)
	assert.Equal(t, "/shots/"+ta.shotID+"/takes/partial", b.PartialURL)
	require.Len(t, b.Takes, 3)
	assert.Equal(t, []string{"A", "B", "C"}, []string{b.Takes[0].Label, b.Takes[1].Label, b.Takes[2].Label})
	assert.Equal(t, "candidate", b.Takes[0].Status)

	first := b.Takes[0]
	require.NotNil(t, first.Scores, "a judged Take has scores")
	assert.InDelta(t, 65, first.Scores.Product, 1e-9, "a high Issue takes 35 off the Product axis")
	assert.InDelta(t, 100, first.Scores.Acting, 1e-9)
	assert.Nil(t, b.Takes[1].Scores, "an unjudged Take has none")
	assert.Equal(t, "/takes/t1/video", first.VideoURL)
	assert.Equal(t, "/takes/t1/thumbnail", first.ThumbnailURL)
	assert.Equal(t, "/takes/t1/select", first.SelectURL)
	assert.Equal(t, "/takes/t1", first.RejectURL)
	assert.InDelta(t, 10, first.FPS, 0.01, "read from the file")
	assert.False(t, first.Proxied)
	assert.True(t, b.Takes[2].Proxied, "4K is played through a proxy")

	assert.Equal(t, http.StatusNotFound, ta.get("/api/v1/shots/nope/takes", false).Code)
}

func TestTakes_SelectMakesTheTakeTheShotsAndRejectExcludesIt(t *testing.T) {
	ta := newTakeApp(t)

	rec := ta.form(http.MethodPost, "/takes/t2/select", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `id="shot-`+ta.shotID+`"`, "answers with the Shot card")
	assert.Equal(t, "selected", ta.statusOf("t2"))
	assert.True(t, ta.takes().Takes[1].Adopted)

	require.Equal(t, http.StatusOK, ta.form(http.MethodPost, "/takes/t1/select", nil).Code)
	assert.Equal(t, "candidate", ta.statusOf("t2"), "the earlier choice is demoted")
	assert.Equal(t, "selected", ta.statusOf("t1"))

	rec = ta.form(http.MethodDelete, "/takes/t1", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "rejected", ta.statusOf("t1"))
	for _, tk := range ta.takes().Takes {
		assert.False(t, tk.Adopted, "the Shot has no Take after its Take was rejected")
	}

	assert.Equal(t, http.StatusNotFound, ta.form(http.MethodPost, "/takes/nope/select", nil).Code)
	assert.Equal(t, http.StatusNotFound, ta.form(http.MethodDelete, "/takes/nope", nil).Code)
}

// A Take button answers with the card it sits on: a card that was not selected stays unselected (no
// aria-current, no storyboard / generate form), so the Shot being viewed keeps its page (#257).
func TestTakes_SelectAndRejectAnswerWithTheCardAsSelectedOrNotLikeTheOneTheyReplace(t *testing.T) {
	ta := newTakeApp(t)
	const current = `aria-current="true"`

	rec := ta.form(http.MethodPost, "/takes/t1/select", url.Values{"selected": {"false"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `id="shot-`+ta.shotID+`"`)
	assert.NotContains(t, rec.Body.String(), current)
	assert.NotContains(t, rec.Body.String(), "/generate", "no generate form on an unselected card")
	assert.Equal(t, "selected", ta.statusOf("t1"), "the Take is selected all the same")

	rec = ta.form(http.MethodPost, "/takes/t2/select", url.Values{"selected": {"true"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), current)

	rec = ta.form(http.MethodDelete, "/takes/t2?selected=false", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), current)

	rec = ta.form(http.MethodDelete, "/takes/t1?selected=true", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), current)
}

func TestTakes_PartialUseJoinsPartsIntoANewSelectedTakeAndAnnouncesItInTheChat(t *testing.T) {
	ta := newTakeApp(t)

	rec := ta.form(http.MethodPost, "/shots/"+ta.shotID+"/takes/partial", url.Values{"instruction": {"Take A の 0-2秒 + Take B の 2-4秒"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	b := ta.takes()
	require.Len(t, b.Takes, 4)
	composed := b.Takes[3]
	assert.Equal(t, "D", composed.Label)
	assert.Equal(t, "selected", composed.Status)
	assert.True(t, composed.Adopted)
	assert.InDelta(t, 3.75, composed.Duration, 0.2, "2 s + 2 s less the 0.25 s cross-fade")
	assert.InDelta(t, 10, composed.FPS, 0.01)

	video := ta.get(composed.VideoURL, false)
	require.Equal(t, http.StatusOK, video.Code)
	assert.Equal(t, "video/mp4", video.Header().Get("Content-Type"))

	chat := ta.get("/projects/"+ta.project, false).Body.String()
	assert.Contains(t, chat, "部分採用 Take")
	assert.Contains(t, chat, "Take A 0.0-2.0秒 + Take B 2.0-4.0秒")
	assert.Contains(t, chat, `video-src="`+composed.VideoURL+`"`)

	var content string
	require.NoError(t, ta.db.Reader().QueryRowContext(context.Background(),
		`SELECT content FROM chat_message WHERE ref_type = 'take' AND ref_id = ?`, composed.ID).Scan(&content))
	assert.Empty(t, domain.DecodeTakeCardBody(content).Badge, "the thread is append-only: the card carries no Take state that excluding the Take would leave stale")
}

func TestTakes_PartialUseAcceptsSegmentsAsJSONAndRefusesWhatCannotBeJoined(t *testing.T) {
	ta := newTakeApp(t)
	post := func(body string) *httptest.ResponseRecorder {
		return ta.request(http.MethodPost, "/shots/"+ta.shotID+"/takes/partial", []byte(body), "application/json", false)
	}

	rec := post(`{"segments":[{"take_id":"t1","start":0,"end":1.5},{"take_id":"t2","start":1,"end":3}]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, ta.takes().Takes, 4)

	for name, body := range map[string]string{
		"one segment":       `{"segments":[{"take_id":"t1","start":0,"end":1.5}]}`,
		"beyond the length": `{"segments":[{"take_id":"t1","start":0,"end":9},{"take_id":"t2","start":1,"end":3}]}`,
		"unknown take":      `{"segments":[{"take_id":"zz","start":0,"end":1.5},{"take_id":"t2","start":1,"end":3}]}`,
		"unreadable words":  `{"instruction":"いい感じに"}`,
		"nothing":           `{}`,
		"broken json":       `{"segments":`,
	} {
		assert.Equal(t, http.StatusUnprocessableEntity, post(body).Code, name)
	}
	assert.Len(t, ta.takes().Takes, 4, "nothing was created by the refused requests")

	ta.form(http.MethodDelete, "/takes/t2", nil)
	rec = post(`{"segments":[{"take_id":"t1","start":0,"end":1.5},{"take_id":"t2","start":1,"end":3}]}`)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "a rejected Take cannot be used")
	assert.Equal(t, http.StatusNotFound, ta.form(http.MethodPost, "/shots/nope/takes/partial", url.Values{"instruction": {"Take A の 0-1秒 + Take B の 0-1秒"}}).Code)
}

func TestTakePreview_4KIsPlayedThroughA1080pProxyAndSeekingIsSupported(t *testing.T) {
	ta := newTakeApp(t)

	rec := ta.get("/takes/t3/video", false)
	require.Equal(t, http.StatusOK, rec.Code)
	proxy := filepath.Join(ta.dir, "assets", ".previews", "t3.mp4")
	info, err := ta.tools.Probe(context.Background(), proxy)
	require.NoError(t, err, "the proxy is a video file next to the assets")
	assert.LessOrEqual(t, info.Height, 1080)
	assert.InDelta(t, 1, info.Duration, 0.2)
	_, err = os.Stat(filepath.Join(ta.dir, "assets", ta.project, "t3.mp4"))
	require.NoError(t, err, "the 4K original is kept")

	// A 1080p Take is served as it is, and a range request is answered with the range.
	req := httptest.NewRequest(http.MethodGet, "/takes/t1/video", nil)
	req.AddCookie(ta.cookie)
	req.Header.Set("Range", "bytes=0-99")
	rec = do(ta.engine, req)
	assert.Equal(t, http.StatusPartialContent, rec.Code)
	assert.Equal(t, 100, rec.Body.Len())
	_, err = os.Stat(filepath.Join(ta.dir, "assets", ".previews", "t1.mp4"))
	assert.True(t, os.IsNotExist(err), "no proxy for 1080p")

	thumb := ta.get("/takes/t1/thumbnail", false)
	require.Equal(t, http.StatusOK, thumb.Code)
	assert.Equal(t, "image/jpeg", thumb.Header().Get("Content-Type"))
	assert.True(t, strings.HasPrefix(thumb.Body.String(), "\xff\xd8"), "a JPEG")

	assert.Equal(t, http.StatusNotFound, ta.get("/takes/nope/video", false).Code)
	assert.Equal(t, http.StatusNotFound, ta.get("/takes/nope/thumbnail", false).Code)
}

func TestTakeComparison_PageHostsTheComparatorAndTheChatCardEmbedsThePlayer(t *testing.T) {
	ta := newTakeApp(t)

	page := ta.get("/shots/"+ta.shotID+"/takes", false).Body.String()
	assert.Contains(t, page, "<html")
	assert.Contains(t, page, `<mam-take-comparator api-url="/api/v1/shots/`+ta.shotID+`/takes">`)
	frag := ta.get("/shots/"+ta.shotID+"/takes", true).Body.String()
	assert.NotContains(t, frag, "<html")
	assert.Contains(t, frag, "mam-take-comparator")
	assert.Equal(t, http.StatusNotFound, ta.get("/shots/nope/takes", false).Code)

	card := domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindTake, RefID: "t1",
		Content: domain.EncodeTakeCardBody(domain.TakeCardBody{
			Title: "Shot 1 · Take 1", ShotID: ta.shotID, VideoURL: domain.TakeVideoURL("t1"), PosterURL: domain.TakeThumbnailURL("t1")})}
	require.NoError(t, ta.svc.Chat.Announce(context.Background(), ta.project, card))
	chat := ta.get("/projects/"+ta.project, false).Body.String()
	assert.Contains(t, chat, `video-src="/takes/t1/video"`)
	assert.Contains(t, chat, `api-url="/api/v1/takes/t1/judge-result"`)
	assert.Contains(t, chat, `poster="/takes/t1/thumbnail"`)
	assert.Contains(t, chat, `href="/shots/`+ta.shotID+`/takes"`, "the card links to the comparison")
	assert.Contains(t, chat, "返信")
}

// pickState is the status of each Take of the Shot, oldest first, and the index of the one the Shot uses (-1: none).
func pickState(t *testing.T, a *app, shotID string) (statuses []string, adopted int) {
	t.Helper()
	rec := a.get("/api/v1/shots/"+shotID+"/takes", false)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var b takeListBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &b))
	adopted = -1
	for i, tk := range b.Takes {
		statuses = append(statuses, tk.Status)
		if tk.Adopted {
			adopted = i
		}
	}
	return statuses, adopted
}

func TestAIPick_AutoAdoptsTheBestTakeThatPassedTheJudge(t *testing.T) {
	j := newJudgeApp(t, goodVerdict, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95)
	j.generateAndJudge()

	statuses, adopted := pickState(t, j.app, j.shotID)
	assert.Equal(t, []string{"ai_pick", "candidate", "candidate"}, statuses, "equal scores: the older Take is the AI Pick")
	assert.Equal(t, 0, adopted, "AUTO adopts the AI Pick")
	assert.Contains(t, j.get("/projects/"+j.project, false).Body.String(), "AI Pick: Take ", "the chat says which Take was adopted")
}

func TestAIPick_TakeSelectionManualOrLockOnlyRecommendsEvenInAuto(t *testing.T) {
	for _, mode := range []string{"manual", "lock"} {
		t.Run(mode, func(t *testing.T) {
			j := newJudgeApp(t, goodVerdict, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95)
			require.Equal(t, http.StatusOK, j.form(http.MethodPut, "/shots/"+j.shotID+"/locks", url.Values{"parameter": {"take_selection"}, "mode": {mode}}).Code)
			j.generateAndJudge()

			statuses, adopted := pickState(t, j.app, j.shotID)
			assert.Equal(t, []string{"ai_pick", "candidate", "candidate"}, statuses, "the Take is still recommended")
			assert.Equal(t, -1, adopted, "Take Selection %s: the AI does not make the Shot use it", mode)
			assert.Contains(t, j.get("/projects/"+j.project, false).Body.String(), "がおすすめです")
		})
	}
}

func TestAIPick_DirectorOnlyRecommendsAndAUserChoiceIsNeverReplaced(t *testing.T) {
	j := newJudgeApp(t, goodVerdict, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95)
	require.Equal(t, http.StatusOK, j.form(http.MethodPut, "/projects/"+j.project+"/mode", url.Values{"mode": {"director"}}).Code)
	ids := j.generateAndJudge()

	statuses, adopted := pickState(t, j.app, j.shotID)
	assert.Equal(t, []string{"ai_pick", "candidate", "candidate"}, statuses)
	assert.Equal(t, -1, adopted, "DIRECTOR leaves the choice to the user")
	assert.Contains(t, j.get("/projects/"+j.project, false).Body.String(), "がおすすめです")

	// The user chooses another Take; judging again does not take the choice back.
	require.Equal(t, http.StatusOK, j.form(http.MethodPost, "/takes/"+ids[2]+"/select", nil).Code)
	require.Equal(t, http.StatusAccepted, j.form(http.MethodPost, "/takes/"+ids[0]+"/judge", nil).Code)
	require.Eventually(t, func() bool { return j.results(ids[0]) == 2 }, 30*time.Second, 10*time.Millisecond)
	statuses, adopted = pickState(t, j.app, j.shotID)
	assert.Equal(t, 2, adopted)
	assert.Equal(t, "selected", statuses[2])
}

func TestAIPick_ATakeThatFailedIsRecommendedButNotAdoptedInAuto(t *testing.T) {
	// The face drifts while the spec locks it: every Take fails. Two references calibrate the threshold.
	j := newJudgeApp(t, goodVerdict, 0.95, 0.95, 0.95, 0.2, 0.2, 0.2, 0.95, 0.95)
	j.addMatchingCanonFace(t)
	j.generateAndJudge()

	statuses, adopted := pickState(t, j.app, j.shotID)
	assert.Equal(t, "ai_pick", statuses[0])
	assert.Equal(t, -1, adopted, "a failed Take is not adopted without the user")
}
