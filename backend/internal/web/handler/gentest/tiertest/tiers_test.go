// Package tiertest runs the three tiers of the image form of the generation test (Issue #977: かんたん / こだわり / ComfyUI) and the buttons of a
// result through the screen, over a real service with fake model backends.
package tiertest_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.ReleaseMode)
	m.Run()
}

type screen struct {
	t      *testing.T
	rig    *gentesttest.Rig
	engine http.Handler
	cookie *http.Cookie
	token  string
}

func newScreen(t *testing.T, opts ...gentesttest.Option) *screen {
	t.Helper()
	rig := gentesttest.New(t, opts...)
	svc := servicetest.New(t, service.Deps{DB: rig.DB, Credentials: credentialtest.New()})
	settings := settingsroutes.New(t.TempDir()+"/llm.json", false, t.TempDir()+"/m.json", false)
	settings.Gentest = rig.Svc
	s := &screen{t: t, rig: rig, engine: router.New(svc, router.Options{Settings: settings})}
	rec := s.send(httptest.NewRequest(http.MethodGet, paths.GenerationTest(), nil))
	require.Equal(t, http.StatusOK, rec.Code)
	for _, c := range rec.Result().Cookies() {
		if c.Name == middleware.CSRFCookieName {
			s.cookie = c
		}
	}
	require.NotNil(t, s.cookie)
	m := regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`).FindStringSubmatch(rec.Body.String())
	require.Len(t, m, 2)
	s.token = m[1]
	return s
}

func (s *screen) send(req *http.Request) *httptest.ResponseRecorder {
	req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	s.engine.ServeHTTP(rec, req)
	return rec
}

// page is a full-page GET.
func (s *screen) page(path string) string {
	s.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(s.cookie)
	rec := s.send(req)
	require.Equal(s.t, http.StatusOK, rec.Code, rec.Body.String())
	return rec.Body.String()
}

// htmx is a request the way the browser's HTMX sends it.
func (s *screen) htmx(method, path string, form url.Values) *httptest.ResponseRecorder {
	s.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	req.Header.Set(middleware.CSRFHeaderName, s.token)
	return s.send(req)
}

// installed makes the models a user would have installed from the model manager (workflow name -> template).
func installed(names ...string) gentesttest.Option {
	templates := map[string]string{
		"flux1-dev": "txt2img-flux-dev", "qwen-image": "txt2img-qwen-image", "sdxl-base": "txt2img-sdxl", "noobai-xl-v11": "txt2img-sdxl",
		"flux2-klein-4b": "txt2img-flux2-klein", "qwen-image-edit-2511": "txt2img-qwen-image-edit-2511",
	}
	got := map[string]string{}
	for _, n := range names {
		got[n] = templates[n]
	}
	return gentesttest.WithInstalled(got)
}

// testid is the page from the element with the data-testid on (a window long enough for what it holds), "" when there is none.
func testid(body, id string) string {
	i := strings.Index(body, `data-testid="`+id+`"`)
	if i < 0 {
		return ""
	}
	return body[i:min(len(body), i+5000)]
}

// 初期表示は「かんたん」: やりたいことの選択肢・品質・サイズ・プロンプトがあり、詳細設定（こだわり）はまだ出ない。
func TestTheFormOpensInTheEasyTier(t *testing.T) {
	s := newScreen(t, installed("sdxl-base"))
	body := s.page(paths.GenerationTest())

	assert.Regexp(t, `aria-current="page"[^>]*data-testid="gt-tier-easy"`, body, "かんたん is the tab that is lit")
	assert.Contains(t, body, `data-testid="gt-tier-detail"`)
	assert.Contains(t, body, `data-testid="gt-tier-comfy"`)
	for _, label := range []string{"人物写真", "アニメ", "風景", "同じ顔で"} {
		assert.Contains(t, body, label)
	}
	assert.Contains(t, body, `data-testid="gt-sizes"`)
	assert.Contains(t, body, `data-testid="gt-easy-prompt"`)
	assert.NotContains(t, body, `data-gt-advanced`, "the detailed settings are the こだわり tier's")
	assert.NotContains(t, body, `data-testid="gt-models"`, "the cards of the models are the こだわり tier's")
}

// 選ばれたモデルは導入済みのものから: 人物写真は候補の先頭から導入済みのもの。名前は小さく出る。品質の 3 ボタンが出る。
func TestAnIntentPicksAnInstalledModelAndOffersTheThreeQualities(t *testing.T) {
	s := newScreen(t, installed("sdxl-base"))
	body := s.page(paths.GenerationTest() + "?tier=easy&intent=photo")

	assert.Contains(t, testid(body, "gt-easy-model"), "sdxl-base")
	assert.NotContains(t, body, `data-testid="gt-easy-nomodel"`)
	quality := testid(body, "gt-quality")
	for _, label := range []string{"速い", "標準", "きれい"} {
		assert.Contains(t, quality, label)
	}
	assert.Regexp(t, `checked[^>]*name="quality"[^>]*value="standard"`, quality, "標準 is lit until one is chosen")
}

// 該当モデルが 1 つも導入されていないときは、理由と導入への案内（モデルマネージャーへのリンク）。見積りは押せない。
func TestWithoutAnInstalledModelTheEasyTierSaysWhyAndWhereToInstall(t *testing.T) {
	s := newScreen(t)
	body := s.page(paths.GenerationTest() + "?tier=easy&intent=photo")

	none := testid(body, "gt-easy-nomodel")
	require.NotEmpty(t, none)
	assert.Contains(t, none, `href="`+paths.ModelManager()+`"`)
	assert.Regexp(t, `data-testid="gt-estimate"[^>]*disabled|disabled[^>]*data-testid="gt-estimate"`, body, "there is nothing to estimate")
	assert.NotContains(t, body, `data-testid="gt-model"`)
}

// 成人向けのモデルは、成人ゲートが閉じているあいだは選ばれない。
func TestAnAdultModelIsNotPickedWhileTheGateIsClosed(t *testing.T) {
	adult := gentesttest.InstalledID("noobai-xl-v11")
	closed := newScreen(t, installed("noobai-xl-v11"), gentesttest.WithHidden(func(id string) bool { return id == adult }))
	assert.Contains(t, testid(closed.page(paths.GenerationTest()+"?tier=easy&intent=anime"), "gt-easy-nomodel"), "モデルマネージャー")

	open := newScreen(t, installed("noobai-xl-v11"))
	assert.Contains(t, testid(open.page(paths.GenerationTest()+"?tier=easy&intent=anime"), "gt-easy-model"), "noobai-xl-v11")
}

// 選んだやりたいことに合わせて、品質のボタンとモデルの行だけを返す（ファイルを選んだ入力を壊さない）。
func TestChoosingWhatToMakeSwapsOnlyThePickedModelAndTheQuality(t *testing.T) {
	s := newScreen(t, installed("sdxl-base", "qwen-image-edit-2511"))
	q := url.Values{"intent": {"face"}, "ref": {"history::x"}}
	rec := s.htmx(http.MethodGet, paths.GenerationTestPart("easy-pick")+"?"+q.Encode(), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `id="gt-easy-pick"`)
	assert.Contains(t, body, "qwen-image-edit-2511")
	assert.NotContains(t, body, `<form`, "only the part is swapped")
}

// 何も触らず見積り・実行したとき、かんたんの選択（人物写真）から決めたモデルで作られ、品質は標準＝今のテンプレートの値のまま（上書きなし）。
func TestAnUntouchedEasyFormRunsTheIntentsModelAtTheTemplatesOwnSettings(t *testing.T) {
	s := newScreen(t, installed("sdxl-base"))
	form := url.Values{"tier": {"easy"}, "intent": {"photo"}, "quality": {"standard"}, "media": {"image"}, "prompt": {"a red fox"}, "negative": {""}, "aspect_ratio": {"1:1"}, "count": {"1"}}
	rec := s.htmx(http.MethodPost, paths.GenerationTestPart("estimate"), form)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `name="model" value="`+gentesttest.InstalledID("sdxl-base")+`"`, "the model of the intent is what is run")
	assert.NotContains(t, body, `name="quality"`, "standard overrides nothing, so nothing is carried")
	assert.NotContains(t, body, `name="steps"`)
}

// きれい / 速い は、確認の hidden にも載り、実行が同じ品質で走る。
func TestAQualityIsCarriedToTheConfirmation(t *testing.T) {
	s := newScreen(t, installed("sdxl-base"))
	form := url.Values{"tier": {"easy"}, "intent": {"photo"}, "quality": {"fast"}, "media": {"image"}, "prompt": {"a red fox"}, "negative": {""}, "aspect_ratio": {"9:16"}, "count": {"1"}}
	rec := s.htmx(http.MethodPost, paths.GenerationTestPart("estimate"), form)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `name="quality" value="fast"`)
	assert.Contains(t, body, `name="aspect_ratio" value="9:16"`)
}

// 「詳細設定を開く」で、かんたんの品質（速い）のステップ数がこだわりの欄に最初から入っている。
func TestTheDetailTierStartsWithTheValuesTheEasyTierChose(t *testing.T) {
	s := newScreen(t, installed("sdxl-base"))
	rec := s.htmx(http.MethodGet, paths.GenerationTestPart("form")+"?"+url.Values{
		"tier": {"detail"}, "media": {"image"}, "intent": {"photo"}, "quality": {"fast"}, "model": {gentesttest.InstalledID("sdxl-base")}, "prompt": {"a red fox"},
	}.Encode(), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	assert.Contains(t, body, `data-gt-refine`)
	assert.Contains(t, body, `data-gt-advanced`)
	assert.Regexp(t, `name="steps"[^>]*value="15"|value="15"[^>]*name="steps"`, body, "速い = 15 steps of the SDXL template (25)")
	assert.Contains(t, testid(body, "gt-carried"), "速い")
	assert.Contains(t, body, `>a red fox</textarea>`, "what was typed stays")
}

// こだわり → かんたん: こだわりで変えた詳細設定（かんたんが持たないもの）は消える。消えることを、戻った画面の冒頭に出す（#1000）。
func TestSwitchingBackToTheEasyTierSaysWhichDetailedSettingsAreGone(t *testing.T) {
	s := newScreen(t, installed("sdxl-base"))
	ask := func(extra url.Values) string {
		q := url.Values{"tier": {"easy"}, "media": {"image"}, "intent": {"photo"}, "prompt": {"a red fox"}, "negative": {"blurry"}}
		for k, v := range extra {
			q[k] = v
		}
		rec := s.htmx(http.MethodGet, paths.GenerationTestPart("form")+"?"+q.Encode(), nil)
		require.Equal(t, http.StatusOK, rec.Code)
		return rec.Body.String()
	}

	body := ask(url.Values{"dropped": {"seed", "steps", "exposed"}, "seed": {"123"}, "steps": {"40"}})
	assert.Contains(t, body, "こだわりで変えた詳細設定（シード・ステップ数・画面に出した値）は、かんたんでは使わないため消えました")
	assert.Contains(t, body, "こだわりへ戻っても元には戻りません")
	assert.Contains(t, body, `>a red fox</textarea>`+"", "the prompt stays")
	assert.NotContains(t, body, `name="seed"`, "and the value really is not in the easy form")
	assert.NotContains(t, body, `name="steps"`)
	assert.Contains(t, body, `value="blurry"`, "the negative prompt is kept, so it is not said")

	assert.NotContains(t, ask(nil), "は、かんたんでは使わないため消えました", "nothing was lost: nothing is said")
	assert.NotContains(t, ask(url.Values{"dropped": {"bogus"}}), "は、かんたんでは使わないため消えました", "a key it does not know says nothing")
	detail := ask(url.Values{"tier": {"detail"}, "dropped": {"seed"}, "model": {gentesttest.InstalledID("sdxl-base")}})
	assert.NotContains(t, detail, "は、かんたんでは使わないため消えました", "only a form of the easy tier says it")
}

// ComfyUI の段は、偽の UI を作らず、今ある画面（ノード・ワークフロー・モデルマネージャー）へのリンクだけを出す。
func TestTheComfyTierLinksToTheScreensThatExist(t *testing.T) {
	s := newScreen(t)
	body := s.page(paths.GenerationTest() + "?tier=comfy")

	assert.Regexp(t, `aria-current="page"[^>]*data-testid="gt-tier-comfy"`, body)
	comfy := testid(body, "gt-comfy")
	require.NotEmpty(t, comfy)
	assert.Contains(t, comfy, `href="`+paths.Nodes()+`"`)
	assert.Contains(t, comfy, `href="`+paths.Workflows()+`"`)
	assert.Contains(t, comfy, `href="`+paths.ModelManager()+`"`)
	assert.NotContains(t, body, `data-testid="gt-form"`, "no form: nothing here is made")
}

// 動画のフォームには段がない（今のまま）。
func TestTheVideoFormHasNoTiers(t *testing.T) {
	s := newScreen(t)
	body := s.page(paths.GenerationTest() + "?media=video")
	assert.NotContains(t, body, `data-testid="gt-tiers"`)
	assert.Contains(t, body, `data-testid="gt-models"`)
}

var _ = gentest.MediaImage
