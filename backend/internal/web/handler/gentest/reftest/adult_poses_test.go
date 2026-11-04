package reftest_test

import (
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// The words that name or show a pose of a suggestive series (sexy, duo) of the fake library (posestest.Seed: S001 and D001) or speak of the series: the
// names (Japanese and English), the numbers, the picture IDs and the offer's own title.
var adultWords = []string{"S字立ち", "Standing S-curve", "向き合い", "Facing each other", "sexy_", "duo_", "S001", "D001", "成人向けのポーズ", "gt-pose-adult"}

func assertNoAdultPose(t *testing.T, where, body string) {
	t.Helper()
	for _, w := range adultWords {
		assert.NotContains(t, body, w, where)
	}
}

func cardsOf(body string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`data-ref="([^"]+)"`).FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// Issue #1020, the screen: with the age gate closed (the way the app starts) the pose dialog says nothing of the suggestive series: not in the
// gallery, not in the list a search returns (by name, by number or by series), not as a picture, not as a row to add, and not as an offer. The files are
// on the disk (installed while the gate was open): the gate alone keeps them back.
func TestTheClosedAgeGateLeavesTheSuggestiveSeriesOutOfThePoseDialog(t *testing.T) {
	s, lib := posesScreen(t, false)
	lib.InstallAdult(t)
	lib.Open.Store(false)

	assertNoAdultPose(t, "the gallery", s.get(paths.GenerationTestPoses()).Body.String())
	for _, q := range []url.Values{{}, {"pose_q": {"S字立ち"}}, {"pose_q": {"S001"}}, {"pose_q": {"sexy"}}, {"pose_class": {"sexy/"}}, {"pose_class": {"duo/"}}, {"pose_kind": {"depth"}}, {"pose_kind": {"normal"}}} {
		body := s.get(paths.GenerationTestPoses("results") + "?" + q.Encode()).Body.String()
		assertNoAdultPose(t, "the results "+q.Encode(), body)
	}
	assert.Equal(t, []string{"pose_library::std_P001_front", "pose_library::std_P002_front", "pose_library::upper_U001_front"},
		cardsOf(s.get(paths.GenerationTestPoses("results")).Body.String()), "the ordinary poses are all there is")

	for _, id := range []string{"sexy_S001_front", "sexy_S001_front.depth", "sexy_S001_front.normal", "duo_D001_front", "duo_D001_front.depth"} {
		img := s.get(paths.GenerationTestPoses("image", id))
		assert.Equal(t, http.StatusNotFound, img.Code, id)
		assert.Empty(t, img.Body.Bytes(), id)
		ref := httpGetRefRow(s, "pose_library::"+id)
		assert.Equal(t, http.StatusNotFound, ref, id)
	}

	refused := s.post(paths.GenerationTestPoses("install-adult"), url.Values{"pose_accept": {"on"}})
	assert.GreaterOrEqual(t, refused.Code, 400, "the service refuses, whatever the screen offers")
	assertNoAdultPose(t, "the refusal", refused.Body.String())
}

func httpGetRefRow(s *screen, ref string) int {
	req, _ := http.NewRequest(http.MethodGet, paths.GenerationTestPart("ref-row")+"?ref="+url.QueryEscape(ref), nil)
	req.AddCookie(s.cookie)
	return s.send(req).Code
}

// With the gate open the dialog offers the series (the agreement again, its own licenses and size), installs them on the agreement, lists them in
// their own groups of the categories with the maps, serves their pictures and adds one as a pose row; closing the gate takes all of it out again.
func TestTheOpenAgeGateOffersInstallsAndListsTheSuggestiveSeries(t *testing.T) {
	s, lib := posesScreen(t, true)
	lib.Open.Store(true)

	offer := s.get(paths.GenerationTestPoses()).Body.String()
	assert.Contains(t, offer, `data-testid="gt-pose-adult"`, "the offer is made while the gate is open")
	assert.Contains(t, offer, `data-testid="gt-pose-adult-accept"`)
	assert.Contains(t, offer, `hx-post="`+paths.GenerationTestPoses("install-adult")+`"`)
	assert.NotContains(t, offer, "S字立ち", "an offer names no pose")
	assert.NotContains(t, offer, "gt-pose-adult-installed")

	refused := s.post(paths.GenerationTestPoses("install-adult"), url.Values{})
	assert.GreaterOrEqual(t, refused.Code, 400)
	assert.Contains(t, refused.Body.String(), `data-testid="gt-pose-adult-error"`, "no agreement: said in words, and the offer stays")
	assert.False(t, lib.Lib.Status().Adult.Installed, "nothing was installed")

	done := s.post(paths.GenerationTestPoses("install-adult"), url.Values{"pose_accept": {"on"}})
	require.Equal(t, http.StatusOK, done.Code, done.Body.String())
	assert.Contains(t, done.Body.String(), `data-testid="gt-pose-adult-installed"`)
	assert.Contains(t, done.Body.String(), `data-testid="gt-pose-adult-remove"`)
	gallery := s.get(paths.GenerationTestPoses()).Body.String()
	assert.Contains(t, gallery, "全身・sexy（成人向け）", "their own groups of the categories")
	assert.Contains(t, gallery, "二人・duo（成人向け）")
	list := s.get(paths.GenerationTestPoses("results")).Body.String()
	assert.Contains(t, list, "S字立ち")
	assert.Contains(t, cardsOf(list), "pose_library::sexy_S001_front")
	byClass := s.get(paths.GenerationTestPoses("results") + "?" + url.Values{"pose_class": {"duo/"}}.Encode()).Body.String()
	assert.Equal(t, []string{"pose_library::duo_D001_front"}, cardsOf(byClass))
	depth := s.get(paths.GenerationTestPoses("results") + "?" + url.Values{"pose_kind": {"depth"}, "pose_q": {"S001"}}.Encode()).Body.String()
	assert.Equal(t, []string{"pose_library::sexy_S001_front.depth"}, cardsOf(depth), "the maps of the series")

	img := s.get(paths.GenerationTestPoses("image", "sexy_S001_front"))
	require.Equal(t, http.StatusOK, img.Code)
	assert.Equal(t, "no-store", img.Header().Get("Cache-Control"), "a suggestive picture is not kept by the browser: the gate may be closed within the hour")
	assert.Equal(t, "private, max-age=3600", s.get(paths.GenerationTestPoses("image", "std_P001_front")).Header().Get("Cache-Control"), "an ordinary one is")
	assert.Equal(t, http.StatusOK, httpGetRefRow(s, "pose_library::sexy_S001_front"))

	lib.Open.Store(false)
	assertNoAdultPose(t, "the gallery, the gate closed again", s.get(paths.GenerationTestPoses()).Body.String())
	assert.Equal(t, http.StatusNotFound, s.get(paths.GenerationTestPoses("image", "sexy_S001_front")).Code)

	lib.Open.Store(true)
	removed := s.post(paths.GenerationTestPoses("remove-adult"), url.Values{})
	require.Equal(t, http.StatusOK, removed.Code, removed.Body.String())
	assert.Contains(t, removed.Body.String(), `data-testid="gt-pose-adult-accept"`, "the offer is back")
	assert.Contains(t, removed.Body.String(), `data-testid="gt-pose-gallery"`, "the main library stays")
	assert.NotContains(t, s.get(paths.GenerationTestPoses("results")).Body.String(), "S字立ち")
}

// A batch run with a pose of a suggestive series is in the history with the pose as an input; with the gate closed its detail page does not show the
// input (no picture, no origin), and the copy of the picture is not served.
func TestTheClosedAgeGateKeepsTheSuggestivePoseOutOfTheHistoryPage(t *testing.T) {
	s, lib := posesScreen(t, false)
	lib.InstallAdult(t)
	req := batchRequest(string(gentesttest.EditModel), gentest.RefInput{Source: gentest.FromPoseLibrary, ID: "sexy_S001_front", Role: gentest.RolePose})
	ref, err := s.rig.Confirmed(req)
	require.NoError(t, err)
	st := s.rig.Wait(ref.BatchID)
	require.Equal(t, gentest.StateSucceeded, st.State, "%+v", st.Results)
	d, err := s.rig.Svc.Result(t.Context(), st.Results[0].ID)
	require.NoError(t, err)
	require.Len(t, d.Inputs, 1)
	inputURL := paths.GenerationTestPart("inputs", d.Inputs[0].ID, "file")
	detail := paths.GenerationTestPart("results", st.Results[0].ID)

	open := s.page(detail)
	assert.Contains(t, open, inputURL, "the gate is open: the input is shown")
	assert.Equal(t, http.StatusOK, s.get(inputURL).Code)

	lib.Open.Store(false)
	closed := s.page(detail)
	assert.NotContains(t, closed, inputURL, "no picture of the pose")
	assert.NotContains(t, closed, "ポーズ集", "no origin")
	assertNoAdultPose(t, "the detail", closed)
	assert.Equal(t, http.StatusNotFound, s.get(inputURL).Code, "and the copy is not served")
}

func batchRequest(model string, refs ...gentest.RefInput) gentest.Request {
	return gentest.Request{Media: "image", Models: []string{model}, Prompts: []gentest.PromptInput{{Prompt: "a red fox"}}, References: refs}
}
