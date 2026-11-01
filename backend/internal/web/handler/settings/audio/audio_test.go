package audio_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/audiosettings"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

type runtimes struct {
	views  map[string]localruntime.View
	refuse map[string]error
}

func (r runtimes) Status(_ context.Context, id string) (localruntime.View, error) {
	return r.views[id], nil
}
func (r runtimes) CheckStart(_ context.Context, id string) error { return r.refuse[id] }

type keys map[string]bool

func (k keys) Configured(provider string) (bool, error) { return k[provider], nil }

// machine is a PC that can run both local providers (installed, weights verified) or one that cannot (macOS).
func machine(supported bool) runtimes {
	view := func(id, name string) localruntime.View {
		v := localruntime.View{ID: id, Name: name, SourceURL: "https://example.test/" + id, VRAM: catalog.VRAM{MinGB: 4, Estimate: id == "gpt-sovits"}}
		v.License = catalog.License{Name: "MIT", URL: "https://example.test/mit"}
		if supported {
			v.Support = rtm.Support{Supported: true, Code: rtm.CodeOK}
			v.Installed = true
			v.Weights.State = localruntime.WeightsReady
			v.Weights.Assets = []localruntime.AssetView{{Name: "whisper-tiny", License: catalog.ResourceLicense{CommercialUse: catalog.CommercialUnclear}}}
		} else {
			v.Support = rtm.Support{Code: rtm.CodeOS, Reason: "macOS では公式の PyTorch 版が使えません。", Fallback: "代わりに、クラウドを使ってください。"}
		}
		return v
	}
	r := runtimes{views: map[string]localruntime.View{"gpt-sovits": view("gpt-sovits", "GPT-SoVITS"), "musetalk": view("musetalk", "MuseTalk 1.5")}, refuse: map[string]error{}}
	if !supported {
		for id, v := range r.views {
			r.refuse[id] = &localruntime.SupportError{Runtime: id, Support: v.Support}
		}
	}
	return r
}

type screen struct {
	t      *testing.T
	engine http.Handler
	cookie *http.Cookie
	token  string
	path   string
}

func newScreen(t *testing.T, rt audiosettings.Runtimes, k audiosettings.Keys) *screen {
	t.Helper()
	svc := servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: credentialtest.New()})
	settings := settingsroutes.New(filepath.Join(t.TempDir(), "llm.json"), false, filepath.Join(t.TempDir(), "models.json"), false)
	path := filepath.Join(t.TempDir(), audiosettings.FileName)
	settings.Audio = audiosettings.New(path, rt, k)
	s := &screen{t: t, engine: router.New(svc, router.Options{Settings: settings}), path: path}
	rec := s.send(httptest.NewRequest(http.MethodGet, paths.Settings(), nil))
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

func (s *screen) page() string {
	rec := s.send(httptest.NewRequest(http.MethodGet, paths.AudioSettings(), nil))
	require.Equal(s.t, http.StatusOK, rec.Code)
	return rec.Body.String()
}

func (s *screen) save(form url.Values, withToken bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, paths.AudioSettings(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	if withToken {
		req.Header.Set(middleware.CSRFHeaderName, s.token)
	}
	return s.send(req)
}

var inputRe = regexp.MustCompile(`<input[^>]*>`)

// radio is the tag of the input (a radio or the checkbox) with the data-testid.
func radio(t *testing.T, html, testid string) string {
	t.Helper()
	for _, tag := range inputRe.FindAllString(html, -1) {
		if strings.Contains(tag, `data-testid="`+testid+`"`) {
			return tag
		}
	}
	require.Failf(t, "no radio", "%s in %s", testid, html)
	return ""
}

func TestAudioPage_DefaultsToTheCloudWithTheLocalStateAndTheSource(t *testing.T) {
	s := newScreen(t, machine(true), keys{"elevenlabs": true})

	html := s.page()

	assert.Contains(t, html, `<title>設定: 音声・口パク | mam</title>`)
	assert.Contains(t, html, "声（セリフ・ナレーション）")
	assert.Contains(t, html, "口パク")
	assert.Contains(t, radio(t, html, "audio-voice_provider-cloud"), "checked", "nothing saved: the cloud")
	assert.NotContains(t, radio(t, html, "audio-voice_provider-local"), "checked")
	assert.NotContains(t, radio(t, html, "audio-voice_provider-local"), "disabled", "this PC can run it")
	assert.Contains(t, radio(t, html, "audio-lip_sync_provider-cloud"), "checked")
	assert.Contains(t, html, "導入済み", "where each local provider stands")
	assert.Contains(t, html, "API キー（elevenlabs）: 設定済み")
	assert.Contains(t, html, "API キー（synclabs）: 未設定")
	assert.Contains(t, html, paths.SettingsCategory(paths.SettingsAPIKeys), "unset keys link to where they are stored")
	assert.Contains(t, html, `href="`+paths.ModelManager()+`"`, "the way to the model manager")
	assert.Contains(t, html, "https://example.test/gpt-sovits", "the source of the program")
	assert.Contains(t, html, "ライセンス:")
	assert.Contains(t, html, "必要なビデオメモリ: 4 GB 以上（mam の推定です。実機では確かめていません）")
	assert.Contains(t, html, "商用利用がはっきりしない重み・依存ファイルが 1 件あります", "shown prominently")
	assert.Contains(t, html, "「whisper-tiny」")
	assert.NotContains(t, radio(t, html, "audio-fallback-input"), "checked", "the fallback is off")
	assert.Contains(t, html, "クラウドの費用は出ません")
	assert.NotContains(t, html, "audio-unsupported", "a PC that can run it has no refusal")
}

// A PC that cannot run the local providers (macOS): the radio is disabled, the reason and what to use are on the screen, and the cloud stays.
func TestAudioPage_AnUnsupportedPCShowsTheReasonAndDisablesTheLocalChoice(t *testing.T) {
	s := newScreen(t, machine(false), nil)

	html := s.page()

	for _, field := range []string{"voice_provider", "lip_sync_provider"} {
		assert.Contains(t, radio(t, html, "audio-"+field+"-local"), "disabled", field)
		assert.Contains(t, radio(t, html, "audio-"+field+"-cloud"), "checked", field)
		assert.Contains(t, html, `data-testid="audio-unsupported-`+field+`"`)
	}
	assert.Contains(t, html, "macOS では公式の PyTorch 版が使えません", "the reason of the runtime manager")
	assert.Contains(t, html, "代わりに、クラウドを使ってください", "and what to use instead")
	assert.Contains(t, html, "この PC では GPT-SoVITS を選べません。ElevenLabs（クラウド） のまま使えます")
	assert.Contains(t, html, "この PC では使えません", "the badge")
}

func TestAudioSave_PersistsTheChoiceAndTheFormShowsIt(t *testing.T) {
	s := newScreen(t, machine(true), keys{"elevenlabs": true, "synclabs": true})

	rec := s.save(url.Values{"voice_provider": {"local"}, "lip_sync_provider": {"synclabs"}, "fallback_to_cloud": {"1"}}, true)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "保存しました。次の音声作成から使われます")
	assert.Contains(t, radio(t, rec.Body.String(), "audio-voice_provider-local"), "checked")
	assert.NotContains(t, radio(t, rec.Body.String(), "audio-voice_provider-cloud"), "checked")
	assert.Contains(t, radio(t, rec.Body.String(), "audio-fallback-input"), "checked")
	raw, err := os.ReadFile(s.path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"voice_provider": "local"`)
	assert.Contains(t, string(raw), `"fallback_to_cloud": true`)
	// A later page load (a restart reads the same file) shows it.
	assert.Contains(t, radio(t, s.page(), "audio-voice_provider-local"), "checked")

	// Unchecked is off: a checkbox that is not in the form is false.
	rec = s.save(url.Values{"voice_provider": {"elevenlabs"}, "lip_sync_provider": {"synclabs"}}, true)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, radio(t, rec.Body.String(), "audio-fallback-input"), "checked")
	assert.Contains(t, radio(t, rec.Body.String(), "audio-voice_provider-cloud"), "checked")
}

func TestAudioSave_AProviderThisPCCannotRunIsRefusedWithItsReasonAndNothingIsWritten(t *testing.T) {
	s := newScreen(t, machine(false), nil)

	rec := s.save(url.Values{"voice_provider": {"local"}, "lip_sync_provider": {"synclabs"}}, true)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "この PC ではローカルの声（GPT-SoVITS）を選べません。macOS では公式の PyTorch 版が使えません")
	assert.Contains(t, rec.Body.String(), `role="alert"`)
	assert.NoFileExists(t, s.path)
}

// A saved local choice on a PC that cannot run it (the file was copied from another PC) is not used: the page says so and the form's
// choice is the cloud, so that saving puts the file right.
func TestAudioPage_ASavedLocalChoiceThatThisPCCannotRunIsSaidSoAndTheFormShowsTheCloud(t *testing.T) {
	s := newScreen(t, machine(false), nil)
	require.NoError(t, os.WriteFile(s.path, []byte(`{"voice_provider":"local"}`), 0o600))

	html := s.page()

	assert.Contains(t, html, "保存済みの設定はローカルですが、この PC では動かせないため、今は使われません")
	assert.Contains(t, radio(t, html, "audio-voice_provider-cloud"), "checked")
	rec := s.save(url.Values{"voice_provider": {"elevenlabs"}, "lip_sync_provider": {"synclabs"}}, true)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestAudioSave_ABrokenFileIsShownAsAProblemAndNotOverwritten(t *testing.T) {
	s := newScreen(t, machine(true), nil)
	require.NoError(t, os.WriteFile(s.path, []byte(`{"voice_provider":`), 0o600))

	assert.Contains(t, s.page(), "設定ファイル（audio.json）を読めません")
	rec := s.save(url.Values{"voice_provider": {"local"}}, true)

	assert.Equal(t, http.StatusConflict, rec.Code)
	raw, err := os.ReadFile(s.path)
	require.NoError(t, err)
	assert.Equal(t, `{"voice_provider":`, string(raw))
}

func TestAudioSave_NeedsTheCSRFToken(t *testing.T) {
	s := newScreen(t, machine(true), nil)

	rec := s.save(url.Values{"voice_provider": {"local"}}, false)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.NoFileExists(t, s.path)
}

func TestAudioScreen_WithoutTheServiceThereIsNoCategoryAndNoRoute(t *testing.T) {
	svc := servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: credentialtest.New()})
	engine := router.New(svc, router.Options{Settings: settingsroutes.New(filepath.Join(t.TempDir(), "llm.json"), false, filepath.Join(t.TempDir(), "models.json"), false)})
	req := httptest.NewRequest(http.MethodGet, paths.AudioSettings(), nil)
	req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}
