package reftest_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/poses/posestest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// #986: the pose picker. posesScreen is the generation test screen with a pose library over a fake GitHub (installed or not).
func posesScreen(t *testing.T, installed bool) (*screen, *posestest.Rig) {
	t.Helper()
	lib := posestest.New(t)
	if installed {
		lib.Install(t)
	}
	return newScreen(t, gentesttest.WithReferenceModels(""), gentesttest.WithPoseLibrary(lib.Lib)), lib
}

func (s *screen) post(path string, form url.Values) *httptest.ResponseRecorder {
	s.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	req.Header.Set(middleware.CSRFHeaderName, s.token)
	return s.send(req)
}

func (s *screen) get(path string) *httptest.ResponseRecorder {
	s.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	return s.send(req)
}

func TestTheComposeFormOffersThePosePickerForAnImageBatchWhenTheAppHasTheLibrary(t *testing.T) {
	s, _ := posesScreen(t, false)
	page := s.page(paths.GenerationTest())
	assert.Contains(t, page, `data-testid="gt-open-poses"`)
	assert.Contains(t, page, `data-gt-pose-dialog`)
	assert.Contains(t, page, paths.GenerationTestPoses())
	assert.NotContains(t, s.page(paths.GenerationTest()+"?media=video"), "gt-open-poses", "a video takes no pose chart")

	without := newScreen(t, gentesttest.WithReferenceModels(""), gentesttest.WithPoseLibrary(nil))
	assert.NotContains(t, without.page(paths.GenerationTest()), "gt-open-poses")
	assert.Equal(t, http.StatusNotFound, without.get(paths.GenerationTestPoses()).Code)
}

// Before the install the dialog says where the files come from, their licenses with the commercial-use answers, and needs the box.
func TestThePoseLibraryIsOfferedWithItsLicensesAndNeedsTheConsentToInstall(t *testing.T) {
	s, _ := posesScreen(t, false)
	body := s.get(paths.GenerationTestPoses()).Body.String()
	assert.Contains(t, body, `data-testid="gt-pose-install"`)
	assert.Contains(t, body, "CC0 1.0")
	assert.Contains(t, body, `data-commercial-badge="yes"`)
	assert.Contains(t, body, `data-testid="gt-pose-accept"`)
	assert.NotContains(t, body, "gt-pose-gallery")

	refused := s.post(paths.GenerationTestPoses("install"), url.Values{})
	assert.GreaterOrEqual(t, refused.Code, 400)
	assert.Contains(t, refused.Body.String(), `data-testid="gt-pose-error"`, "the refusal is said in words, and the offer stays")
	assert.Contains(t, refused.Body.String(), `data-testid="gt-pose-install"`)

	ok := s.post(paths.GenerationTestPoses("install"), url.Values{"pose_accept": {"on"}})
	require.Equal(t, http.StatusOK, ok.Code, ok.Body.String())
	assert.Contains(t, ok.Body.String(), `data-testid="gt-pose-gallery"`)
	assert.Contains(t, s.get(paths.GenerationTestPoses()).Body.String(), `data-testid="gt-pose-gallery"`, "installed: the gallery from now on")
}

func TestTheGalleryListsTheGeneralPosesAndFiltersThem(t *testing.T) {
	s, _ := posesScreen(t, true)
	cards := func(q url.Values) []string {
		body := s.get(paths.GenerationTestPoses("results") + "?" + q.Encode()).Body.String()
		var out []string
		for _, m := range regexp.MustCompile(`data-ref="([^"]+)"`).FindAllStringSubmatch(body, -1) {
			out = append(out, m[1])
		}
		return out
	}
	assert.ElementsMatch(t, []string{"pose_library::std_P001_front", "pose_library::std_P002_front", "pose_library::upper_U001_front"}, cards(url.Values{}), "the front, and no suggestive pose")
	assert.ElementsMatch(t, []string{"pose_library::std_P001_front", "pose_library::std_P002_side", "pose_library::upper_U001_front"}, cards(url.Values{"pose_angle": {"side"}}),
		"the angle chosen where the pose has it, else the picture it has")
	assert.Equal(t, []string{"pose_library::std_P001_front"}, cards(url.Values{"pose_q": {"sitting"}}), "the English name")
	assert.Equal(t, []string{"pose_library::std_P001_front"}, cards(url.Values{"pose_q": {"仰け反り"}}), "the Japanese name")
	assert.Equal(t, []string{"pose_library::upper_U001_front"}, cards(url.Values{"pose_class": {"upper/かわいい系"}}))
	assert.Empty(t, cards(url.Values{"pose_q": {"nothing like it"}}))
	assert.Len(t, cards(url.Values{"pose_class": {"nonsense"}, "pose_angle": {"nonsense"}}), 3, "what is not understood is no filter")

	full := s.get(paths.GenerationTestPoses()).Body.String()
	assert.Contains(t, full, `data-testid="gt-pose-search"`)
	assert.Contains(t, full, `data-testid="gt-pose-class"`)
	assert.Contains(t, full, `data-testid="gt-pose-angles"`)
}

// #1020: the kind of picture chosen in the dialog (line art, depth, normal) lists the poses that have a map of it, at the angle asked for or
// the one the pose has, with the ID of the map; a pose without any map of the kind is not listed, and nothing reaches the suggestive poses.
func TestTheGalleryListsTheDepthAndNormalMapsOfThePosesThatHaveOne(t *testing.T) {
	s, _ := posesScreen(t, true)
	cards := func(q url.Values) []string {
		body := s.get(paths.GenerationTestPoses("results") + "?" + q.Encode()).Body.String()
		var out []string
		for _, m := range regexp.MustCompile(`data-ref="([^"]+)"`).FindAllStringSubmatch(body, -1) {
			out = append(out, m[1])
		}
		return out
	}
	assert.ElementsMatch(t, []string{"pose_library::std_P001_front.depth", "pose_library::std_P002_front.depth"}, cards(url.Values{"pose_kind": {"depth"}}),
		"no depth map for the upper pose")
	assert.ElementsMatch(t, []string{"pose_library::std_P001_front.depth", "pose_library::std_P002_side.depth"}, cards(url.Values{"pose_kind": {"depth"}, "pose_angle": {"side"}}))
	assert.Equal(t, []string{"pose_library::std_P001_diag.normal"}, cards(url.Values{"pose_kind": {"normal"}, "pose_angle": {"diag"}}), "only P001 has a normal map")
	assert.Len(t, cards(url.Values{"pose_kind": {"clay"}}), 3, "what is not understood is the line art")

	full := s.get(paths.GenerationTestPoses()).Body.String()
	assert.Contains(t, full, `data-testid="gt-pose-kinds"`)
	assert.Regexp(t, `name="pose_kind"[^>]*value="line"[^>]*checked`, full, "the line art is the default")
	assert.Contains(t, full, `value="depth"`)
	assert.Contains(t, full, `value="normal"`)
}

// #1020: a map is served like a line picture, and is the reference of the role pose with its own label and thumbnail.
func TestADepthMapIsServedAndAddedAsAPoseReference(t *testing.T) {
	s, _ := posesScreen(t, true)
	ok := s.get(paths.GenerationTestPoses("image", "std_P001_front.depth"))
	require.Equal(t, http.StatusOK, ok.Code)
	assert.Equal(t, posestest.MapPicture("depth", "std_P001_front"), ok.Body.Bytes())
	assert.Equal(t, posestest.MapPicture("normal", "std_P001_diag"), s.get(paths.GenerationTestPoses("image", "std_P001_diag.normal")).Body.Bytes())
	for _, id := range []string{"std_P001_diag.depth", "upper_U001_front.depth", "std_P003_front.depth", "std_P001_front.clay"} {
		assert.Equal(t, http.StatusNotFound, s.get(paths.GenerationTestPoses("image", id)).Code, id+": no such map")
	}

	body := s.fragment(paths.GenerationTestPart("ref-row") + "?ref=" + url.QueryEscape("pose_library::std_P001_front.depth") + "&model=" + url.QueryEscape(string(gentesttest.EditModel)))
	assert.Contains(t, body, `value="pose_library::std_P001_front.depth"`)
	assert.Contains(t, body, paths.GenerationTestPoses("image", "std_P001_front.depth"))
	assert.Contains(t, body, "深度マップ")
	assert.Regexp(t, `<option value="pose"[^>]*selected`, body)

	form := batchForm(string(gentesttest.EditModel))
	form["ref"] = []string{"pose_library::std_P001_front.depth"}
	form.Set("role_pose_library::std_P001_front.depth", "pose")
	rec := s.run(s.confirmed(form))
	assert.Less(t, rec.Code, 400, rec.Body.String())
}

func TestThePictureOfAPoseIsServedAndNothingElse(t *testing.T) {
	s, _ := posesScreen(t, true)
	ok := s.get(paths.GenerationTestPoses("image", "std_P001_front"))
	require.Equal(t, http.StatusOK, ok.Code)
	assert.Equal(t, "image/webp", ok.Header().Get("Content-Type"))
	assert.Equal(t, posestest.Picture("std_P001_front"), ok.Body.Bytes())
	for _, id := range []string{"std_P003_front", "std_P001_below", "..%2F..%2Fetc%2Fpasswd", "nonsense"} {
		assert.Equal(t, http.StatusNotFound, s.get(paths.GenerationTestPoses("image", id)).Code, id)
	}
}

// The base model's license is said in the pose dialog too: a model that reads the pose and may not be used commercially says 商用NG.
func TestThePoseDialogSaysWhetherTheTickedModelMayBeUsedCommercially(t *testing.T) {
	modellicense.SetResolver(func(id string) (modellicense.Info, bool) {
		if id == string(gentesttest.EditModel) {
			return modellicense.Info{Commercial: modellicense.No, LicenseName: "Qwen RESEARCH", Notice: "研究目的のみです"}, true
		}
		return modellicense.Info{}, false
	})
	t.Cleanup(func() { modellicense.SetResolver(nil) })
	s, _ := posesScreen(t, true)

	read := s.get(paths.GenerationTestPoses() + "?model=" + url.QueryEscape(string(gentesttest.EditModel))).Body.String()
	assert.Contains(t, read, `data-testid="gt-pose-model"`)
	assert.Contains(t, read, `data-commercial="no"`)
	assert.Contains(t, read, `data-commercial-badge="no"`)
	assert.Contains(t, read, "研究目的のみです")

	whole := s.get(paths.GenerationTestPoses() + "?model=" + url.QueryEscape(string(gentesttest.Img2ImgModel))).Body.String()
	assert.Contains(t, whole, `data-testid="gt-pose-models-unsupported"`, "a model that cannot read a pose takes the chart as a whole picture")
	assert.NotContains(t, whole, `data-testid="gt-pose-model"`)

	none := s.get(paths.GenerationTestPoses()).Body.String()
	assert.Contains(t, none, `data-testid="gt-pose-models-none"`)
	assert.Contains(t, none, "ポーズを読めるモデル", "the models that read a pose are named")

	// the same, before the install
	un, _ := posesScreen(t, false)
	assert.Contains(t, un.get(paths.GenerationTestPoses()+"?model="+url.QueryEscape(string(gentesttest.EditModel))).Body.String(), `data-commercial-badge="no"`)
}

// "追加" fetches the row of the chosen picture: the role is the pose, and the thumbnail is the picture of the library.
func TestAddingAPosePictureDrawsARowWithTheRolePose(t *testing.T) {
	s, _ := posesScreen(t, true)
	body := s.fragment(paths.GenerationTestPart("ref-row") + "?ref=" + url.QueryEscape("pose_library::std_P002_side") + "&model=" + url.QueryEscape(string(gentesttest.EditModel)))
	assert.Contains(t, body, `value="pose_library::std_P002_side"`)
	assert.Contains(t, body, paths.GenerationTestPoses("image", "std_P002_side"))
	assert.Regexp(t, `<option value="pose"[^>]*selected`, body)
	assert.Contains(t, body, "腕を組んで立つ")

	for _, id := range []string{"std_P003_front", "nonsense"} {
		req := httptest.NewRequest(http.MethodGet, paths.GenerationTestPart("ref-row")+"?ref="+url.QueryEscape("pose_library::"+id), nil)
		req.AddCookie(s.cookie)
		assert.Equal(t, http.StatusNotFound, s.send(req).Code, id)
	}
}

// The batch with the pose picture goes through the form: estimate, confirm, run.
func TestThePoseChartTravelsInTheFormToTheRun(t *testing.T) {
	s, _ := posesScreen(t, true)
	form := batchForm(string(gentesttest.EditModel))
	form["ref"] = []string{"pose_library::std_P001_front"}
	form.Set("role_pose_library::std_P001_front", "pose")
	hidden := s.confirmed(form)
	assert.Equal(t, []string{"pose_library::std_P001_front"}, hidden["ref"])
	assert.Equal(t, "pose", hidden.Get("role_pose_library::std_P001_front"))
	rec := s.run(hidden)
	assert.Less(t, rec.Code, 400, rec.Body.String())
}

// #994: the gallery has a button that deletes the library (and the offer comes back); the install panel polls how far the download is.
func TestThePoseLibraryCanBeDeletedFromTheGalleryAndTheInstallPanelPollsTheProgress(t *testing.T) {
	s, lib := posesScreen(t, true)
	gallery := s.get(paths.GenerationTestPoses()).Body.String()
	assert.Contains(t, gallery, `data-testid="gt-pose-remove"`)
	assert.Contains(t, gallery, "hx-confirm=", "a deletion is confirmed")
	assert.Contains(t, gallery, `hx-post="`+paths.GenerationTestPoses("remove")+`"`)

	removed := s.post(paths.GenerationTestPoses("remove"), url.Values{})
	require.Equal(t, http.StatusOK, removed.Code, removed.Body.String())
	assert.Contains(t, removed.Body.String(), `data-testid="gt-pose-install"`, "the offer to install comes back")
	assert.NotContains(t, removed.Body.String(), "gt-pose-gallery")
	assert.False(t, lib.Lib.Status().Installed)
	assert.Equal(t, http.StatusNotFound, s.get(paths.GenerationTestPoses("image", "std_P001_front")).Code, "the pictures are gone")
	assert.Equal(t, http.StatusOK, s.post(paths.GenerationTestPoses("remove"), url.Values{}).Code, "a library that is gone is removed again without an error")

	offer := s.get(paths.GenerationTestPoses()).Body.String()
	assert.Contains(t, offer, `data-testid="gt-pose-progress"`)
	assert.Contains(t, offer, `hx-get="`+paths.GenerationTestPoses("progress")+`"`)
	idle := s.get(paths.GenerationTestPoses("progress"))
	require.Equal(t, http.StatusOK, idle.Code)
	assert.Empty(t, strings.TrimSpace(idle.Body.String()), "nothing runs: nothing is said")

	var said []string
	require.NoError(t, lib.Lib.Install(t.Context(), true, func(_, _ int64) {
		said = append(said, strings.TrimSpace(s.get(paths.GenerationTestPoses("progress")).Body.String()))
	}))
	require.NotEmpty(t, said)
	assert.Regexp(t, `^ダウンロード中: .+ / .+（\d+%）$`, said[len(said)-1], "the line the panel polls while the install runs")
}
