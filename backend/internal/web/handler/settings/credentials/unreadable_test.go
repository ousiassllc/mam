package credentials_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/credential/probe"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/credentials"
)

// A key store that cannot be read leaves the cards without a state, but the cards that the choice of the API provider shows are the same as when it can be read
// (spec/components/settings-screen-categories.md §1.0): the aggregators' cards are not there under the official provider.
func TestPanel_AnUnreadableKeyStoreStillFollowsTheChoiceOfTheProvider(t *testing.T) {
	r := newProviderRig(t, false)
	r.keys.FailWith(errors.New("no secret service"))

	official := providers(r.panel())

	assert.Contains(t, official, "kling")
	assert.NotContains(t, official, "openrouter")
	assert.NotContains(t, official, "fal")
	assert.NotContains(t, official, "replicate")

	require.NoError(t, r.models.SaveVideoProvider("openrouter"))

	chosen := providers(r.panel())

	require.NotEmpty(t, chosen)
	assert.Equal(t, "openrouter", chosen[0], "the key of the chosen provider comes first")
	assert.NotContains(t, chosen, "kling")
	assert.NotContains(t, chosen, "kling4")
	assert.NotContains(t, chosen, "fal")
}

// afterWrite is a key store that works until a key was saved or deleted and cannot be read after it: the answer of the save is the whole panel, drawn without a state.
type afterWrite struct {
	credential.Store
	broken bool
}

func (s *afterWrite) Set(p, k string) error {
	err := s.Store.Set(p, k)
	s.broken = err == nil
	return err
}
func (s *afterWrite) Delete(p string) error {
	err := s.Store.Delete(p)
	s.broken = err == nil
	return err
}
func (s *afterWrite) Has(p string) (bool, error) {
	if s.broken {
		return false, errors.New("no secret service")
	}
	return s.Store.Has(p)
}

// The answer of a save (or a delete) after which the key store cannot be read is the whole panel too, and it follows the choice of the API provider like the panel does.
func TestSave_APanelAnsweredAfterAnUnreadableKeyStoreFollowsTheChoiceOfTheProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for name, do := range map[string]func(*afterWrite) *httptest.ResponseRecorder{
		"save": func(store *afterWrite) *httptest.ResponseRecorder {
			return serve(store, http.MethodPut, "/credentials/openai", url.Values{"api_key": {"sk-test-key-value"}})
		},
		"delete": func(store *afterWrite) *httptest.ResponseRecorder {
			require.NoError(t, store.Store.Set("openai", "sk-test-key-value"))
			return serve(store, http.MethodDelete, "/credentials/openai", nil)
		},
	} {
		t.Run(name, func(t *testing.T) {
			store := &afterWrite{Store: credentialtest.New()}

			rec := do(store)

			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			got := providers(rec.Body.String())
			assert.Contains(t, got, "kling", "the official provider shows the keys of the vendors")
			assert.NotContains(t, got, "openrouter")
			assert.NotContains(t, got, "fal")
			assert.NotContains(t, got, "replicate")
		})
	}
}

func serve(store credential.Store, method, target string, form url.Values) *httptest.ResponseRecorder {
	engine := gin.New()
	k := credentials.Keys{Svc: service.NewCredentialService(store), Tester: probe.NewTester(store), Models: modelsettings.New(filepath.Join(os.TempDir(), "mam-no-models.json"), false)}
	credentials.Register(engine, k, func(*gin.Context) {})
	req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}
