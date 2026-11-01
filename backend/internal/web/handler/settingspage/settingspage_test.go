package settingspage_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/audiosettings"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// screen is the router over the settings of a test: with settings (the LLM / model categories) or without.
type screen struct {
	t      *testing.T
	engine http.Handler
}

func newScreen(t *testing.T, tweak func(*settingsroutes.Services)) *screen {
	t.Helper()
	svc := servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: credentialtest.New()})
	opts := router.Options{}
	if tweak != nil {
		settings := settingsroutes.New(filepath.Join(t.TempDir(), "llm.json"), false, filepath.Join(t.TempDir(), "models.json"), false)
		tweak(settings)
		opts.Settings = settings
	}
	return &screen{t: t, engine: router.New(svc, opts)}
}

func (s *screen) get(target string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	s.engine.ServeHTTP(rec, req)
	return rec
}

var (
	navRe     = regexp.MustCompile(`(?s)<nav aria-label="設定".*?</nav>`)
	currentRe = regexp.MustCompile(`<a href="([^"]+)"[^>]*aria-current="page"[^>]*>([^<]+)</a>`)
	titleRe   = regexp.MustCompile(`<title>([^<]*)</title>`)
	h2Re      = regexp.MustCompile(`<h2 id="settings-category-title"[^>]*>([^<]*)</h2>`)
)

// withSettings builds the router with the LLM and model settings (which settingsroutes.New gives) and nothing more.
func withSettings(*settingsroutes.Services) {}

// Each category is a page at /settings/<slug>: its own <title> and h2, the navigation named "設定" with the category as
// the current page (aria-current) and every other category as a plain link, and one h1 over all of them.
func TestPages_EveryCategoryHasItsURLTitleHeadingAndCurrentLinkInTheNavigation(t *testing.T) {
	s := newScreen(t, withSettings)
	tests := []struct{ slug, title, h2 string }{
		{paths.SettingsOverview, "設定", "概要"},
		{paths.SettingsAPIKeys, "設定: API キーとログイン", "API キーとログイン"},
		{paths.SettingsLLM, "設定: テキスト生成（LLM）", "テキスト生成（LLM）"},
		{paths.SettingsModels, "設定: 画像・動画モデル", "画像・動画モデル"},
		{paths.SettingsUpdate, "設定: バージョンとアップデート", "バージョンとアップデート"},
	}
	for _, tc := range tests {
		t.Run("/settings/"+tc.slug, func(t *testing.T) {
			rec := s.get(paths.SettingsCategory(tc.slug))
			require.Equal(t, http.StatusOK, rec.Code)
			body := rec.Body.String()

			assert.Equal(t, tc.title+" | mam", titleRe.FindStringSubmatch(body)[1])
			assert.Equal(t, tc.h2, h2Re.FindStringSubmatch(body)[1])
			assert.Equal(t, 1, strings.Count(body, "<h1 "))
			nav := navRe.FindString(body)
			require.NotEmpty(t, nav)
			assert.Equal(t, 1, strings.Count(nav, `aria-current="page"`), "exactly one current category")
			assert.Equal(t, []string{paths.SettingsCategory(tc.slug), tc.h2}, currentRe.FindStringSubmatch(nav)[1:])
			assert.NotContains(t, nav, "hx-", "a category is a page load, not a boosted request")
			assert.Contains(t, body, `id="mam-modal"`, "the page holds the modal its buttons fill")
			assert.NotContains(t, body, `rel="canonical"`)
		})
	}
}

// The navigation has the categories that the services have, in the order of the spec; the others have neither a link nor a page.
func TestPages_ACategoryWithoutItsServiceHasNoLinkAndNoPage(t *testing.T) {
	without := newScreen(t, nil)
	body := without.get(paths.Settings()).Body.String()
	nav := navRe.FindString(body)
	for _, label := range []string{"概要", "API キーとログイン", "バージョンとアップデート"} {
		assert.Contains(t, nav, ">"+label+"<")
	}
	for _, label := range []string{"テキスト生成（LLM）", "画像・動画モデル", "ローカル・代替モデル", "MCP サーバー"} {
		assert.NotContains(t, nav, label)
	}
	for _, slug := range []string{paths.SettingsLLM, paths.SettingsModels, paths.SettingsLocalModels, paths.SettingsMCP, "no-such-category"} {
		assert.Equal(t, http.StatusNotFound, without.get(paths.SettingsCategory(slug)).Code, slug)
	}

	with := newScreen(t, withSettings)
	order := navRe.FindString(with.get(paths.Settings()).Body.String())
	last := -1
	for _, label := range []string{"概要", "API キーとログイン", "テキスト生成（LLM）", "画像・動画モデル", "バージョンとアップデート"} {
		at := strings.Index(order, ">"+label+"<")
		assert.Greater(t, at, last, label+" comes in the order of the spec")
		last = at
	}
}

// An HTMX request (a poll, the re-read after an action) gets the body of the category without the frame; a navigation or a
// history restore gets the page. The answer depends on the headers, so Vary names them.
func TestPages_HTMXGetsTheBodyAndANavigationOrHistoryRestoreGetsThePage(t *testing.T) {
	s := newScreen(t, withSettings)
	for _, slug := range []string{paths.SettingsOverview, paths.SettingsAPIKeys, paths.SettingsLLM, paths.SettingsUpdate} {
		target := paths.SettingsCategory(slug)
		fragment := s.get(target, "HX-Request", "true")
		require.Equal(t, http.StatusOK, fragment.Code, target)
		assert.NotContains(t, fragment.Body.String(), "<html", target)
		assert.NotContains(t, fragment.Body.String(), "<nav", target)
		assert.Contains(t, fragment.Body.String(), `id="settings-category"`, target)
		assert.Contains(t, fragment.Header().Values("Vary"), "HX-Request, HX-History-Restore-Request", target)

		restore := s.get(target, "HX-Request", "true", "HX-History-Restore-Request", "true")
		assert.Contains(t, restore.Body.String(), "<html", target+": a history restore swaps the whole body")
	}
}

// The ChatGPT section is a part of the API key category: opened without HTMX it is that page, with the canonical link
// to the URL of the category (not a redirect); with HTMX it stays the fragment that the poll of a login reads.
func TestPages_TheChatGPTURLWithoutHTMXIsTheAPIKeyPageWithACanonicalLink(t *testing.T) {
	s := newScreen(t, func(settings *settingsroutes.Services) {
		settings.ChatGPT = chatgpt.New(chatgpt.Options{Keys: credentialtest.New(), DataDir: t.TempDir(), LockPath: filepath.Join(t.TempDir(), "lock")})
	})
	for _, target := range []string{paths.ChatGPTSettings(), paths.ChatGPTModels(), paths.ChatGPTCodexModels(), paths.ChatGPTLogin("official")} {
		rec := s.get(target)
		require.Equal(t, http.StatusOK, rec.Code, target)
		body := rec.Body.String()
		assert.Contains(t, body, `<link rel="canonical" href="/settings/api-keys">`, target)
		assert.Equal(t, "API キーとログイン", currentRe.FindStringSubmatch(navRe.FindString(body))[2], target)
		assert.Contains(t, body, `id="chatgpt-panel"`, target)
	}
	fragment := s.get(paths.ChatGPTSettings(), "HX-Request", "true").Body.String()
	assert.True(t, strings.HasPrefix(fragment, `<section id="chatgpt-panel"`), "the poll's fragment is the cards alone")
	assert.Equal(t, http.StatusNotFound, s.get(paths.ChatGPTLogin("nobody"), "HX-Request", "true").Code, "HTMX asks for a modal that is not there: a 404 modal")
	assert.Equal(t, http.StatusOK, s.get(paths.ChatGPTLogin("nobody")).Code, "typed into the address bar, it is the page like any other modal address")
}

// The overview has one card for each category the screen has, in the order of the navigation, and the model manager last;
// every card is a plain link whose name is its title.
func TestOverview_HasACardForEachCategoryAndTheModelManagerLast(t *testing.T) {
	s := newScreen(t, withSettings)
	body := s.get(paths.Settings()).Body.String()
	cards := regexp.MustCompile(`<a id="tool-([a-z-]+)" href="([^"]+)" data-testid="tool-card"`).FindAllStringSubmatch(body, -1)
	var got [][2]string
	for _, c := range cards {
		got = append(got, [2]string{c[1], c[2]})
	}
	assert.Equal(t, [][2]string{
		{"api-keys", "/settings/api-keys"}, {"llm", "/settings/llm"}, {"models", "/settings/models"},
		{"update", "/settings/update"}, {"model-manager", paths.ModelManager()},
	}, got)
	assert.Contains(t, body, "API キー 0 / 14 件", "the state of the keys is drawn with the card")
	assert.Contains(t, body, `hx-get="/tools/model-manager/status"`, "the model manager's state is read after the page is shown")
	assert.Contains(t, body, "別の画面を開きます")
	assert.NotContains(t, body, `id="credential-panel"`)
}

// The "音声・口パク" category is a page of its own after "ローカル・代替モデル"'s place in the order (before MCP and ffmpeg), has a card in
// the overview, and is left out (link, card and page) without its service.
func TestAudioCategory_HasItsPageNavigationLinkAndOverviewCard(t *testing.T) {
	s := newScreen(t, func(s *settingsroutes.Services) {
		s.Audio = audiosettings.New(filepath.Join(t.TempDir(), audiosettings.FileName), nil, nil)
	})

	rec := s.get(paths.SettingsCategory(paths.SettingsAudio))
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Equal(t, "設定: 音声・口パク | mam", titleRe.FindStringSubmatch(body)[1])
	assert.Equal(t, "音声・口パク", h2Re.FindStringSubmatch(body)[1])
	nav := navRe.FindString(body)
	assert.Equal(t, []string{paths.SettingsCategory(paths.SettingsAudio), "音声・口パク"}, currentRe.FindStringSubmatch(nav)[1:])
	assert.Equal(t, 1, strings.Count(nav, `aria-current="page"`))

	order := navRe.FindString(s.get(paths.Settings()).Body.String())
	last := -1
	for _, label := range []string{"概要", "API キーとログイン", "テキスト生成（LLM）", "画像・動画モデル", "音声・口パク", "バージョンとアップデート"} {
		at := strings.Index(order, ">"+label+"<")
		assert.Greater(t, at, last, label+" comes in the order of the spec")
		last = at
	}

	overview := s.get(paths.Settings()).Body.String()
	assert.Contains(t, overview, `<a id="tool-audio" href="/settings/audio" data-testid="tool-card"`)
	assert.Contains(t, overview, "声: ElevenLabs（クラウド）", "the card says who speaks")
	assert.Contains(t, overview, "口パク: Sync Labs（クラウド）")

	without := newScreen(t, withSettings)
	assert.NotContains(t, navRe.FindString(without.get(paths.Settings()).Body.String()), "音声・口パク")
	assert.Equal(t, http.StatusNotFound, without.get(paths.SettingsCategory(paths.SettingsAudio)).Code)
}
