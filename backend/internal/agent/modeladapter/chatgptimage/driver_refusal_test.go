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

// These tests are about which answers of the Codex endpoint count as the refusal of a driver (issue #907, #951); the driver
// choice itself is in driver_test.go.

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
	first := chatgptimage.FallbackDrivers()[0]
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
		{"curly double quotes", http.StatusBadRequest, func(d string) string {
			return "The “" + d + "” model is not supported when using Codex with a ChatGPT account"
		}, true},
		{"curly single quotes", http.StatusBadRequest, func(d string) string {
			return "The ‘" + d + "’ model is not supported when using Codex with a ChatGPT account"
		}, true},
		{"the apostrophe of doesn't opens no quote", http.StatusBadRequest, func(d string) string {
			return "The model doesn't allow it: the '" + d + "' model is not supported when using Codex with a ChatGPT account"
		}, true},
		{"doesn't after the name", http.StatusBadRequest, func(d string) string {
			return "Model '" + d + "' not found; it doesn't match any model of the account"
		}, true},
		{"curly quotes of another model", http.StatusBadRequest, func(string) string {
			return "The “gpt-image-2” model is not supported when using Codex with a ChatGPT account"
		}, false},
		{"upper case name", http.StatusBadRequest, func(d string) string {
			return "The '" + strings.ToUpper(d) + "' model is not supported when using Codex with a ChatGPT account"
		}, true},
		{"unquoted unsupported model", http.StatusBadRequest, func(d string) string { return "Unsupported model: " + d }, true},
		{"unquoted, 404, upper case, full stop", http.StatusNotFound, func(d string) string {
			return "Unsupported model: " + strings.ToUpper(d) + "."
		}, true},
		{"unquoted, another model", http.StatusBadRequest, func(string) string { return "Unsupported model: gpt-image-2" }, false},
		{"unquoted, a longer name that starts with the driver", http.StatusBadRequest, func(d string) string {
			return "Unsupported model: " + d + "-luna"
		}, false},
		{"unquoted, no model named", http.StatusBadRequest, func(string) string { return "Unsupported model:" }, false},
		{"unquoted with other words around it", http.StatusBadRequest, func(d string) string {
			return "Unsupported model: " + d + " for this account"
		}, false},
		{"unquoted, 500", http.StatusInternalServerError, func(d string) string { return "Unsupported model: " + d }, false},
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
				assert.Equal(t, chatgptimage.FallbackDrivers()[:2], asked, "the next driver was tried")
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

// The refusal may also arrive in a 200 stream (response.failed or error): it is read as a 400, so it moves on only when it
// names the driver that was tried.
func TestDriver_ARefusalInTheStreamIsReadLikeTheStatusOne(t *testing.T) {
	first := chatgptimage.FallbackDrivers()[0]
	for _, tc := range []struct {
		name    string
		event   func(model string) string
		moveOn  bool
		message string
	}{
		{"response.failed", func(m string) string {
			return `{"type":"response.failed","response":{"error":{"message":"The '` + m + `' model is not supported when using Codex with a ChatGPT account"}}}`
		}, true, ""},
		{"error event", func(m string) string {
			return `{"type":"error","error":{"message":"The '` + m + `' model is not supported when using Codex with a ChatGPT account"}}`
		}, true, ""},
		{"response.failed of another model", func(string) string {
			return `{"type":"response.failed","response":{"error":{"message":"The 'gpt-image-2' model is not supported when using Codex with a ChatGPT account"}}}`
		}, false, "gpt-image-2"},
		{"response.failed, not a refusal", func(string) string {
			return `{"type":"response.failed","response":{"error":{"message":"The server had an error while processing your request"}}}`
		}, false, "server had an error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var asked []string
			c := newCodexWith(t, func(_ *codex, w http.ResponseWriter, r *http.Request) {
				var body struct {
					Model string `json:"model"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				mu.Lock()
				asked = append(asked, body.Model)
				n := len(asked)
				mu.Unlock()
				w.Header().Set("Content-Type", "text/event-stream")
				if n == 1 {
					data(w, `{"type":"response.created"}`, tc.event(body.Model))
					return
				}
				data(w, `{"type":"response.created"}`, imageDone("png"), `{"type":"response.completed"}`)
			})
			res, err := chatgptimage.New(&source{}, chatgptimage.Options{BaseURL: c.URL, Stall: time.Second}).
				GenerateImage(context.Background(), "gpt-image-2", modeladapter.ImageRequest{Prompt: "x"})
			mu.Lock()
			defer mu.Unlock()
			if tc.moveOn {
				require.NoError(t, err)
				assert.Equal(t, png, res.Data)
				assert.Equal(t, chatgptimage.FallbackDrivers()[:2], asked, "the next driver was tried")
				return
			}
			var pe *modeladapter.ProviderError
			require.ErrorAs(t, err, &pe)
			require.NotErrorIs(t, err, chatgpt.ErrNoImageModel, "the endpoint's own error is not hidden")
			assert.Contains(t, pe.Message, tc.message)
			assert.Equal(t, []string{first}, asked, "no other driver is tried")
		})
	}
}
