package chatgptimage_test

import (
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptimage"
	"github.com/ousiassllc/mam/backend/internal/chatgpt"
)

// These tests are about what the backend remembers and refuses around the driver (issue #1071): a pinned driver that is
// refused, the remembered driver per account, and the copy that FallbackDrivers returns.

// The pinned driver is the operator's own choice: when it is refused the failure says the pin is wrong (updating mam does
// not help), not that no model was found.
func TestDriver_ARefusedPinSaysThePinIsWrong(t *testing.T) {
	d := newDrivers(t, "gpt-pinned")
	_, err := draw(d.backend(chatgptimage.Options{Driver: "gpt-pinned"}))
	require.ErrorIs(t, err, chatgpt.ErrImageModelPinned)
	require.NotErrorIs(t, err, chatgpt.ErrNoImageModel)
	require.ErrorIs(t, err, modeladapter.ErrRejected)
	var pe *modeladapter.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, http.StatusBadRequest, pe.Status)
	assert.Contains(t, pe.Message, "gpt-pinned")
	assert.Contains(t, pe.Message, "is not supported when using Codex with a ChatGPT account", "and what the endpoint said")
}

// A refusal that names another model (the image tool's gpt-image-2) says nothing about the remembered driver: the call
// ends with the endpoint's own error, and the driver is kept.
func TestDriver_AnotherModelsRefusalKeepsTheRememberedDriver(t *testing.T) {
	var imageRefused atomic.Bool
	var mu sync.Mutex
	var seen []string
	c := newCodexWith(t, func(_ *codex, w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		seen = append(seen, body.Model)
		mu.Unlock()
		if imageRefused.Load() {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"detail":"The 'gpt-image-2' model is not supported when using Codex with a ChatGPT account"}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		data(w, `{"type":"response.created"}`, imageDone("png"), `{"type":"response.completed"}`)
	})
	c.setModels(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"slug":"gpt-a","visibility":"list","priority":1},{"slug":"gpt-b","visibility":"list","priority":2}]}`))
	})
	asked := func() []string {
		mu.Lock()
		defer mu.Unlock()
		got := seen
		seen = nil
		return got
	}
	b := chatgptimage.New(&source{}, chatgptimage.Options{BaseURL: c.URL, Stall: time.Second})
	_, err := draw(b)
	require.NoError(t, err)
	assert.Equal(t, []string{"gpt-a"}, asked())

	imageRefused.Store(true)
	_, err = draw(b)
	var pe *modeladapter.ProviderError
	require.ErrorAs(t, err, &pe)
	require.NotErrorIs(t, err, chatgpt.ErrNoImageModel, "the endpoint's own error is not hidden")
	assert.Equal(t, []string{"gpt-a"}, asked(), "the remembered driver was asked once, no other one is tried")

	imageRefused.Store(false)
	_, err = draw(b)
	require.NoError(t, err)
	assert.Equal(t, []string{"gpt-a"}, asked(), "the remembered driver is still the first")
	assert.EqualValues(t, 1, c.listed.Load(), "the list was read once")
}

// The remembered driver belongs to the ChatGPT account that it worked for, and a sign-out (credentials that cannot be
// read) makes the next call pick again.
func TestDriver_TheRememberedOneBelongsToTheAccountAndEndsWithTheSignIn(t *testing.T) {
	d := newDrivers(t, "gpt-a")
	d.setModels(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"slug":"gpt-a","visibility":"list","priority":1},{"slug":"gpt-b","visibility":"list","priority":2}]}`))
	})
	src := &source{account: "acct-1"}
	b := d.backendWith(src, chatgptimage.Options{})
	_, err := draw(b)
	require.NoError(t, err)
	assert.Equal(t, []string{"gpt-a", "gpt-b"}, d.asked())

	d.reset()
	_, err = draw(b)
	require.NoError(t, err)
	assert.Equal(t, []string{"gpt-b"}, d.asked(), "the same account: the remembered driver goes first")

	d.reset()
	src.account = "acct-2"
	_, err = draw(b)
	require.NoError(t, err)
	assert.Equal(t, []string{"gpt-a", "gpt-b"}, d.asked(), "another account: what worked for acct-1 is not used")

	d.reset()
	_, err = draw(b)
	require.NoError(t, err)
	assert.Equal(t, []string{"gpt-b"}, d.asked(), "the new account's own driver is remembered")

	d.reset()
	src.err = chatgpt.ErrNotSignedIn
	_, err = draw(b)
	require.ErrorIs(t, err, chatgpt.ErrNotSignedIn)
	assert.Empty(t, d.asked())

	src.err = nil
	_, err = draw(b)
	require.NoError(t, err)
	assert.Equal(t, []string{"gpt-a", "gpt-b"}, d.asked(), "after the sign-out the driver is picked again, even for the same account")
}

// FallbackDrivers is a copy: changing what it returns changes nothing the backend tries.
func TestFallbackDrivers_CannotBeChangedByTheCaller(t *testing.T) {
	first := chatgptimage.FallbackDrivers()[0]
	got := chatgptimage.FallbackDrivers()
	got[0] = "changed"
	assert.Equal(t, first, chatgptimage.FallbackDrivers()[0])

	d := newDrivers(t)
	_, err := draw(d.backend(chatgptimage.Options{}))
	require.NoError(t, err)
	assert.Equal(t, []string{first}, d.asked())
}
