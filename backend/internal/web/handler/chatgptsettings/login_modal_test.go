package chatgptsettings_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// The modal of a pending login asks again every two seconds with poll=1: while the login is pending it is the progress, and
// once it has ended the answer closes the modal and replaces the cards (it worked) or says so (it did not).
func TestChatGPTSettings_TheLoginModalPollsAndClosesWhenTheLoginHasWorked(t *testing.T) {
	g := newGate(t)
	s := newScreen(t, true, g.tweak)
	require.NoError(t, s.svc.SetImageEnabled(true, true))
	s.auth.PendingPolls = 1
	rec := s.act(http.MethodPost, paths.ChatGPTLogin("image"), url.Values{"method": {"device"}})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `hx-get="/settings/chatgpt/image/login?poll=1"`, "the modal asks again")
	assert.Contains(t, rec.Body.String(), `hx-trigger="every 2s"`)
	assert.Empty(t, rec.Header().Get("HX-Trigger-After-Swap"), "the modal stays open while the login is pending")

	pending := s.modal(paths.ChatGPTLogin("image") + "?poll=1")
	assert.Contains(t, pending, "ABCD-1234")
	assert.Contains(t, pending, "この画面を閉じても、ログインは続きます")

	g.open()
	s.waitFor("codex@example.com")
	done := s.act(http.MethodGet, paths.ChatGPTLogin("image")+"?poll=1", nil)
	require.Equal(t, http.StatusOK, done.Code)
	assert.Equal(t, "modal-close", done.Header().Get("HX-Trigger-After-Swap"))
	assert.Contains(t, done.Body.String(), `<section id="chatgpt-panel"`)
	assert.Contains(t, done.Body.String(), `hx-swap-oob="true"`, "the cards replace themselves")
	assert.Contains(t, done.Body.String(), "codex@example.com")
	assert.NotContains(t, done.Body.String(), "ABCD-1234")
	s.noTokens(done.Body.String())
}

func TestChatGPTSettings_ACancelledLoginIsTheFormAgainWithTheCardsReplaced(t *testing.T) {
	s := newScreen(t, true, newGate(t).tweak)
	s.auth.PendingPolls = 1 << 20
	require.NoError(t, s.svc.SetImageEnabled(true, true))
	s.act(http.MethodPost, paths.ChatGPTLogin("image"), url.Values{"method": {"device"}})
	s.act(http.MethodPost, paths.ChatGPTLoginCancel("image"), nil)

	rec := s.act(http.MethodGet, paths.ChatGPTLogin("image")+"?poll=1", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get("HX-Trigger-After-Swap"), "not signed in: nothing to close for")
	assert.Contains(t, rec.Body.String(), `data-testid="chatgpt-login-image-device"`, "the choice of the method is back")
	assert.NotContains(t, rec.Body.String(), "ABCD-1234")
}

// A login that could not start is the modal again with why (and the status the old section answered), not a toast.
func TestChatGPTSettings_ALoginThatCannotStartIsTheModalWithTheReason(t *testing.T) {
	s := newScreen(t, true)
	rec := s.act(http.MethodPost, paths.ChatGPTLogin("image"), url.Values{"method": {"device"}})

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), `id="mam-modal-error"`)
	assert.Contains(t, rec.Body.String(), "画像生成（非公式）がオフです")
	assert.Empty(t, rec.Header().Get("HX-Trigger-After-Swap"))
}

// The model list is a modal: read only, with the way back, and the reason when it could not be read.
func TestChatGPTSettings_TheModelListIsAModalThatSaysWhyWhenItCannotBeRead(t *testing.T) {
	s := newScreen(t, true)
	body := s.modal(paths.ChatGPTModels())

	assert.Contains(t, body, `id="mam-modal-title"`)
	assert.Contains(t, body, "モデルの一覧を取得できませんでした")
	assert.Contains(t, body, "data-modal-close")
	assert.NotContains(t, body, `<form`)
}

func TestChatGPTSettings_CancelAndModels(t *testing.T) {
	s := newScreen(t, true, newGate(t).tweak) // never opened: the login stays pending until it is cancelled
	s.auth.PendingPolls = 1 << 20
	require.NoError(t, s.svc.SetImageEnabled(true, true))
	s.act(http.MethodPost, paths.ChatGPTLogin("image"), url.Values{"method": {"device"}})
	assert.Contains(t, s.modal(paths.ChatGPTLogin("image")), "ABCD-1234", "the code is in the modal, reopened while the login is pending")
	assert.Contains(t, s.panel(), "ログイン待ち")
	assert.NotContains(t, s.panel(), "ABCD-1234", "the card says it waits; the code is only in the modal")
	rec := s.act(http.MethodPost, paths.ChatGPTLoginCancel("image"), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "ログインを中止しました")
	assert.Equal(t, "modal-close", rec.Header().Get("HX-Trigger-After-Swap"), "the modal closes and the cards replace themselves")
	assert.Contains(t, rec.Body.String(), `hx-swap-oob="true"`)
	assert.NotContains(t, rec.Body.String(), "ABCD-1234")

	req := httptest.NewRequest(http.MethodGet, paths.ChatGPTModels(), nil)
	req.Header.Set("HX-Request", "true")
	rec = s.send(req)
	assert.NotContains(t, rec.Body.String(), "chatgpt-model-list", "not signed in: no list")
}

func TestChatGPTSettings_ModelsListsTheVisibleModelsOfTheSignedInPlan(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"models":[{"slug":"plan-model-1","display_name":"Plan One","visibility":"list"},{"slug":"hidden-model","visibility":"hide"}]}`)
	}))
	defer api.Close()
	s := newScreen(t, true, func(o *chatgpt.Options) { o.APIBase = api.URL })

	req := httptest.NewRequest(http.MethodGet, paths.ChatGPTModels(), nil)
	req.Header.Set("HX-Request", "true")
	assert.Contains(t, s.send(req).Body.String(), "モデルの一覧を取得できませんでした", "not signed in")

	s.act(http.MethodPost, paths.ChatGPTLogin("official"), url.Values{"method": {"browser"}})
	s.waitFor("me@example.com")
	rec := s.send(req)
	assert.Contains(t, rec.Body.String(), "plan-model-1")
	assert.Contains(t, rec.Body.String(), "Plan One")
	assert.NotContains(t, rec.Body.String(), "hidden-model")
	s.noTokens(rec.Body.String())
}

// An unknown kind is a 404 modal that says so when HTMX asks, and the page of the category when the address is typed in,
// like the other modals of the screen.
func TestChatGPTSettings_TheLoginModalOfAnUnknownKindIs404ForHTMXAndThePageOtherwise(t *testing.T) {
	s := newScreen(t, true)

	rec := s.act(http.MethodGet, paths.ChatGPTLogin("nobody"), nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "対象が見つかりません")
	assert.Contains(t, rec.Body.String(), "data-modal-close", "the modal can be closed")

	rec = s.send(httptest.NewRequest(http.MethodGet, paths.ChatGPTLogin("nobody"), nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "<html", "typed into the address bar, it is the page of its category")
}
