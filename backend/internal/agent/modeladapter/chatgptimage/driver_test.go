package chatgptimage_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptimage"
	"github.com/ousiassllc/mam/backend/internal/chatgpt"
)

// notSupported is the answer of the Codex endpoint for a model a ChatGPT account may no longer use (issue #907).
func notSupported(w http.ResponseWriter, model string) {
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write([]byte(`{"detail":"The '` + model + `' model is not supported when using Codex with a ChatGPT account."}`))
}

// drivers is a Codex endpoint whose /responses refuses the models in refused and draws png for any other. seen is every
// driver that was asked for, in order.
type drivers struct {
	*codex
	mu      sync.Mutex
	refused map[string]bool
	seen    []string
}

func newDrivers(t *testing.T, refused ...string) *drivers {
	t.Helper()
	d := &drivers{refused: map[string]bool{}}
	for _, m := range refused {
		d.refused[m] = true
	}
	d.codex = newCodexWith(t, func(c *codex, w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		d.mu.Lock()
		d.seen = append(d.seen, body.Model)
		refuse := d.refused[body.Model]
		d.mu.Unlock()
		c.n.Add(1)
		if refuse {
			notSupported(w, body.Model)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		data(w, `{"type":"response.created"}`, imageDone("png"), `{"type":"response.completed"}`)
	})
	return d
}

func (d *drivers) asked() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.seen...)
}

func (d *drivers) refuse(models ...string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.refused = map[string]bool{}
	for _, m := range models {
		d.refused[m] = true
	}
}

func (d *drivers) reset() {
	d.mu.Lock()
	d.seen = nil
	d.mu.Unlock()
}

func (d *drivers) backend(o chatgptimage.Options) *chatgptimage.Backend {
	o.BaseURL = d.URL
	o.Stall = time.Second
	return chatgptimage.New(&source{}, o)
}

func draw(b *chatgptimage.Backend) (modeladapter.ImageResult, error) {
	return b.GenerateImage(context.Background(), "gpt-image-2", modeladapter.ImageRequest{Prompt: "x"})
}

// The retired model is answered with the 400 of issue #907: the next driver is tried, and the call succeeds.
func TestDriver_MovesOnWhenTheEndpointSaysTheModelIsNotSupported(t *testing.T) {
	d := newDrivers(t, chatgptimage.FallbackDrivers[0], chatgptimage.FallbackDrivers[1])
	res, err := draw(d.backend(chatgptimage.Options{}))
	require.NoError(t, err)
	assert.Equal(t, png, res.Data)
	assert.Equal(t, chatgptimage.FallbackDrivers[:3], d.asked(), "no model list: the fallback drivers in order, until one is accepted")
	assert.EqualValues(t, 1, d.listed.Load(), "the list was tried first")
}

// The models of the account are read from the Codex model list: hidden ones are skipped, the others go by priority.
func TestDriver_IsPickedFromTheCodexModelList(t *testing.T) {
	d := newDrivers(t, "gpt-old", "gpt-older")
	var query, auth, account string
	d.setModels(func(w http.ResponseWriter, r *http.Request) {
		d.codex.mu.Lock()
		query, auth, account = r.URL.RawQuery, r.Header.Get("Authorization"), r.Header.Get("ChatGPT-Account-Id")
		d.codex.mu.Unlock()
		_, _ = w.Write([]byte(`{"models":[
			{"slug":"gpt-new-mini","visibility":"list","supported_in_api":true,"priority":30},
			{"slug":"codex-auto-review","visibility":"hide","supported_in_api":true,"priority":1},
			{"slug":"gpt-older","visibility":"list","supported_in_api":true,"priority":5},
			{"slug":"gpt-old","visibility":"list","supported_in_api":true,"priority":2},
			{"slug":"gpt-new","visibility":"list","supported_in_api":false,"priority":10}]}`))
	})
	res, err := draw(d.backend(chatgptimage.Options{ClientVersion: "0.170.1"}))
	require.NoError(t, err)
	assert.Equal(t, png, res.Data)
	assert.Equal(t, []string{"gpt-old", "gpt-older", "gpt-new"}, d.asked(), "by priority, the hidden model left out, the working one stops the walk")
	d.codex.mu.Lock()
	defer d.codex.mu.Unlock()
	assert.Equal(t, "client_version=0.170.1", query, "the list is asked as the Codex CLI asks it")
	assert.Equal(t, "Bearer "+token, auth)
	assert.Equal(t, "acct-7", account)
	assert.NotContains(t, d.asked(), chatgptimage.FallbackDrivers[0], "a readable list replaces the fallback drivers")
}

// A list that cannot be used (an error, a broken body, no model the picker shows) is no failure: the fallback drivers serve.
func TestDriver_AnUnusableModelListLeavesTheFallbackDrivers(t *testing.T) {
	for name, reply := range map[string]func(w http.ResponseWriter, r *http.Request){
		"server error": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
		"not json":     func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) },
		"all hidden": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"models":[{"slug":"codex-auto-review","visibility":"hide","priority":1}]}`))
		},
		"empty": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"models":[]}`)) },
	} {
		t.Run(name, func(t *testing.T) {
			d := newDrivers(t)
			d.setModels(reply)
			_, err := draw(d.backend(chatgptimage.Options{}))
			require.NoError(t, err)
			assert.Equal(t, chatgptimage.FallbackDrivers[:1], d.asked())
		})
	}
}

// The driver that worked is used again without reading the list; once it stops working the list is read again.
func TestDriver_TheWorkingOneIsRememberedAndRepickedWhenItStops(t *testing.T) {
	d := newDrivers(t, "gpt-a")
	d.setModels(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"slug":"gpt-a","visibility":"list","priority":1},{"slug":"gpt-b","visibility":"list","priority":2},{"slug":"gpt-c","visibility":"list","priority":3}]}`))
	})
	b := d.backend(chatgptimage.Options{})
	_, err := draw(b)
	require.NoError(t, err)
	assert.Equal(t, []string{"gpt-a", "gpt-b"}, d.asked())
	assert.EqualValues(t, 1, d.listed.Load())

	d.reset()
	_, err = draw(b)
	require.NoError(t, err)
	assert.Equal(t, []string{"gpt-b"}, d.asked(), "the remembered driver goes first, the refused gpt-a is not asked again")
	assert.EqualValues(t, 1, d.listed.Load(), "no new read of the list while the driver works")

	d.reset()
	d.refuse("gpt-a", "gpt-b")
	_, err = draw(b)
	require.NoError(t, err)
	assert.Equal(t, []string{"gpt-b", "gpt-a", "gpt-c"}, d.asked(), "the driver stopped working: it is dropped and the list is walked again")
	assert.EqualValues(t, 2, d.listed.Load())

	d.reset()
	_, err = draw(b)
	require.NoError(t, err)
	assert.Equal(t, []string{"gpt-c"}, d.asked(), "the new driver is the remembered one")
}

// Options.Driver is an override: it is used as it is, no list is read and no other model is tried.
func TestDriver_ThePinnedOneIsUsedAsItIs(t *testing.T) {
	d := newDrivers(t)
	_, err := draw(d.backend(chatgptimage.Options{Driver: "gpt-pinned"}))
	require.NoError(t, err)
	assert.Equal(t, []string{"gpt-pinned"}, d.asked())
	assert.Zero(t, d.listed.Load())

	d.reset()
	d.refuse("gpt-pinned")
	_, err = draw(d.backend(chatgptimage.Options{Driver: "gpt-pinned"}))
	require.ErrorIs(t, err, chatgpt.ErrNoImageModel)
	assert.Equal(t, []string{"gpt-pinned"}, d.asked(), "a pinned driver has no fallback")
	assert.Zero(t, d.listed.Load())
}

// Every driver refused: one failure that says so, a 400 that is a refusal (no retry helps), with no vendor text to show.
func TestDriver_AllRefusedIsOneExplainableFailure(t *testing.T) {
	d := newDrivers(t, chatgptimage.FallbackDrivers...)
	_, err := draw(d.backend(chatgptimage.Options{}))
	require.ErrorIs(t, err, chatgpt.ErrNoImageModel)
	require.ErrorIs(t, err, modeladapter.ErrRejected)
	assert.False(t, modeladapter.IsUnavailable(err), "the breaker does not count a refusal of the model")
	var pe *modeladapter.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, http.StatusBadRequest, pe.Status)
	for _, m := range chatgptimage.FallbackDrivers {
		assert.Contains(t, pe.Message, m, "the log says which models were tried")
	}
	assert.Contains(t, pe.Message, "is not supported when using Codex with a ChatGPT account", "and what the endpoint said")
	assert.NotContains(t, err.Error(), token)
	assert.Equal(t, chatgptimage.FallbackDrivers, d.asked())
}

// Only the refusal of the model moves on: a moderation stop, a rate limit and an unrelated 400 end the call as they are.
func TestDriver_OtherFailuresDoNotMoveOn(t *testing.T) {
	for name, reply := range map[string]func(w http.ResponseWriter){
		"unrelated 400": func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"detail":"Invalid size for the image_generation tool"}`))
		},
		"other model error": func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"detail":"The model produced an invalid tool call"}`))
		},
		"rate limit": func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"The 'gpt-6.1-sol' model is not supported"}}`))
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := newCodexWith(t, func(c *codex, w http.ResponseWriter, _ *http.Request) {
				c.n.Add(1)
				reply(w)
			})
			_, err := chatgptimage.New(&source{}, chatgptimage.Options{BaseURL: c.URL}).GenerateImage(context.Background(), "gpt-image-2", modeladapter.ImageRequest{Prompt: "x"})
			var pe *modeladapter.ProviderError
			require.ErrorAs(t, err, &pe)
			require.NotErrorIs(t, err, chatgpt.ErrNoImageModel)
			assert.EqualValues(t, 1, c.n.Load(), "one call: no other driver is tried")
		})
	}
}

// Whether an answer moves on to the next driver depends on the model it names: only a refusal of the driver that was tried
// does. Another model (the image tool's own gpt-image-2, issue #907 review), or one whose name only starts with the
// driver's, ends the call with the endpoint's own text instead of hiding it behind ErrNoImageModel.
func TestDriver_OnlyARefusalNamingTheDriverMovesOn(t *testing.T) {
	first := chatgptimage.FallbackDrivers[0]
	for _, tc := range []struct {
		name    string
		status  int
		message func(driver string) string
		moveOn  bool
	}{
		{"400 not supported", http.StatusBadRequest, func(d string) string {
			return "The '" + d + "' model is not supported when using Codex with a ChatGPT account"
		}, true},
		{"404 not supported", http.StatusNotFound, func(d string) string {
			return "The '" + d + "' model is not supported when using Codex with a ChatGPT account"
		}, true},
		{"400 not found", http.StatusBadRequest, func(d string) string { return "Model \"" + d + "\" not found" }, true},
		{"404 not found", http.StatusNotFound, func(d string) string { return "The model `" + d + "` was not found" }, true},
		{"400 does not exist", http.StatusBadRequest, func(d string) string {
			return "The model ‘" + d + "’ does not exist or you do not have access to it"
		}, true},
		{"404 does not exist", http.StatusNotFound, func(d string) string { return "The model '" + d + "' does not exist" }, true},
		{"upper case name", http.StatusBadRequest, func(d string) string {
			return "The '" + strings.ToUpper(d) + "' model is not supported when using Codex with a ChatGPT account"
		}, true},
		{"another model of the call: the image tool", http.StatusBadRequest, func(string) string {
			return "The 'gpt-image-2' model is not supported when using Codex with a ChatGPT account"
		}, false},
		{"another model, not found", http.StatusNotFound, func(string) string { return "Model \"gpt-image-2\" not found" }, false},
		{"a longer name that starts with the driver", http.StatusBadRequest, func(d string) string {
			return "The '" + d + "-luna' model is not supported when using Codex with a ChatGPT account"
		}, false},
		{"no model named", http.StatusBadRequest, func(string) string {
			return "The model is not supported when using Codex with a ChatGPT account"
		}, false},
		{"401", http.StatusUnauthorized, func(d string) string { return "The '" + d + "' model is not supported" }, false},
		{"403", http.StatusForbidden, func(d string) string { return "The '" + d + "' model is not supported" }, false},
		{"429", http.StatusTooManyRequests, func(d string) string { return "The '" + d + "' model is not supported" }, false},
		{"500", http.StatusInternalServerError, func(d string) string { return "The '" + d + "' model is not supported" }, false},
		{"503", http.StatusServiceUnavailable, func(d string) string { return "The '" + d + "' model does not exist" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var asked []string
			c := newCodexWith(t, func(c *codex, w http.ResponseWriter, r *http.Request) {
				var body struct {
					Model string `json:"model"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				c.n.Add(1)
				mu.Lock()
				asked = append(asked, body.Model)
				n := len(asked)
				mu.Unlock()
				if n == 1 { // the first driver is the one the answer is about
					w.WriteHeader(tc.status)
					msg, _ := json.Marshal(map[string]string{"detail": tc.message(body.Model)})
					_, _ = w.Write(msg)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				data(w, `{"type":"response.created"}`, imageDone("png"), `{"type":"response.completed"}`)
			})
			res, err := chatgptimage.New(&source{}, chatgptimage.Options{BaseURL: c.URL, Stall: time.Second}).
				GenerateImage(context.Background(), "gpt-image-2", modeladapter.ImageRequest{Prompt: "x"})
			mu.Lock()
			defer mu.Unlock()
			require.Equal(t, first, asked[0])
			if tc.moveOn {
				require.NoError(t, err)
				assert.Equal(t, png, res.Data)
				assert.Equal(t, chatgptimage.FallbackDrivers[:2], asked, "the next driver was tried")
				return
			}
			var pe *modeladapter.ProviderError
			require.ErrorAs(t, err, &pe)
			require.NotErrorIs(t, err, chatgpt.ErrNoImageModel, "the endpoint's own error is not hidden")
			assert.Equal(t, tc.status, pe.Status)
			assert.Equal(t, []string{first}, asked, "no other driver is tried")
		})
	}
}
