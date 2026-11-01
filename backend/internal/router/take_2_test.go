package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

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
	assert.Equal(t, []string{"sandbox", ""}, []string{rec.Header().Get("Content-Security-Policy"), rec.Header().Get("Content-Disposition")}, "a served file carries its own sandbox policy, and the preview is played, not downloaded")
	_, err = os.Stat(filepath.Join(ta.dir, "assets", ".previews", "t1.mp4"))
	assert.True(t, os.IsNotExist(err), "no proxy for 1080p")
	thumb := ta.get("/takes/t1/thumbnail", false)
	require.Equal(t, http.StatusOK, thumb.Code)
	assert.Equal(t, "image/jpeg", thumb.Header().Get("Content-Type"))
	assert.Equal(t, "sandbox", thumb.Header().Get("Content-Security-Policy"))
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
	require.EventuallyWithT(t, func(c *assert.CollectT) { assert.Equal(c, 2, j.results(c, ids[0])) }, 30*time.Second, 10*time.Millisecond)
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
