// Package chatroutes_test checks the chat cards that carry out a setting (the budget of #1139, the masked field of a key of #1140) through the whole HTTP stack. It is a
// package of its own because router is at its size limit.
package chatroutes_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/chatwire"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/testenv"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
)

type client struct {
	t      *testing.T
	engine *gin.Engine
	cookie *http.Cookie
	token  string
	keys   *credentialtest.Memory
}

var (
	metaRe      = regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`)
	formAction  = regexp.MustCompile(`hx-post="(/projects/[^"/]+/messages/[^"/]+/secret)"`)
	redirectURL = regexp.MustCompile(`^/projects/(.+)$`)
)

func newClient(t *testing.T) *client {
	t.Helper()
	testenv.Parallel(t)
	keys := credentialtest.New()
	svc := servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: keys, AssetsDir: filepath.Join(t.TempDir(), "assets")})
	chatwire.Connect(svc, chatwire.Settings{})
	c := &client{t: t, engine: router.New(svc, router.Options{}), keys: keys}
	rec := c.do(httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == middleware.CSRFCookieName {
			c.cookie = ck
		}
	}
	require.NotNil(t, c.cookie)
	m := metaRe.FindStringSubmatch(rec.Body.String())
	require.Len(t, m, 2)
	c.token = m[1]
	return c
}

func (c *client) do(req *http.Request) *httptest.ResponseRecorder {
	req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	c.engine.ServeHTTP(rec, req)
	return rec
}

func (c *client) form(method, path string, v url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(v.Encode())))
	req.AddCookie(c.cookie)
	req.Header.Set(middleware.CSRFHeaderName, c.token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	return c.do(req)
}

func (c *client) get(path string) string { return c.fetch(path, true) }

// fetch is a GET; htmx is whether it is a request of HTMX (a fragment) or a navigation (the whole page).
func (c *client) fetch(path string, htmx bool) string {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(c.cookie)
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	return c.do(req).Body.String()
}

// project is a new Project made by the first message "美容液の縦型15秒CMを作りたい"; projectOf makes one by the words it is given (the multipart form is what the
// new-project page posts).
func (c *client) project() string { return c.projectOf("美容液の縦型15秒CMを作りたい") }

func (c *client) projectOf(first string) string {
	body := strings.NewReader("--b\r\nContent-Disposition: form-data; name=\"content\"\r\n\r\n" + first + "\r\n--b--\r\n")
	req := httptest.NewRequest(http.MethodPost, "/projects", body)
	req.AddCookie(c.cookie)
	req.Header.Set(middleware.CSRFHeaderName, c.token)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=b")
	req.Header.Set("HX-Request", "true")
	rec := c.do(req)
	require.Equal(c.t, http.StatusCreated, rec.Code, rec.Body.String())
	m := redirectURL.FindStringSubmatch(rec.Header().Get("HX-Redirect"))
	require.Len(c.t, m, 2)
	return m[1]
}

// The key of a chat card is typed into its masked field and posted to the card's own endpoint: the answers and the thread never show it, and the key store has it.
func TestSecret_TheCardTakesTheKeyAtItsOwnEndpointAndTheThreadNeverShowsIt(t *testing.T) {
	c := newClient(t)
	id := c.project()
	const secret = "sk-very-secret-value-0123456789"

	rec := c.form(http.MethodPost, "/projects/"+id+"/messages", url.Values{"content": {"OpenAI の API キーを設定したい"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	m := formAction.FindStringSubmatch(rec.Body.String())
	require.NotNil(t, m, "the card has a form that posts to its own endpoint: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), `type="password"`)

	rec = c.form(http.MethodPost, m[1], url.Values{"secret": {secret}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), secret)
	assert.Contains(t, rec.Body.String(), "OpenAI」の API キーを保存しました")
	got, err := c.keys.Get("openai")
	require.NoError(t, err)
	assert.Equal(t, secret, got)
	assert.NotContains(t, c.get("/projects/"+id+"/messages"), secret)
	assert.NotContains(t, c.get("/projects/"+id), secret)

	rec = c.form(http.MethodPost, m[1], url.Values{"secret": {secret}})
	assert.Equal(t, http.StatusConflict, rec.Code, "the card is answered once")
	assert.NotContains(t, rec.Body.String(), secret)
}

func TestSecret_ARefusedKeyIsAToastWithoutTheKeyAndTheCardStaysOpen(t *testing.T) {
	c := newClient(t)
	id := c.project()
	rec := c.form(http.MethodPost, "/projects/"+id+"/messages", url.Values{"content": {"OpenAI の API キーを設定したい"}})
	m := formAction.FindStringSubmatch(rec.Body.String())
	require.NotNil(t, m)

	rec = c.form(http.MethodPost, m[1], url.Values{"secret": {"two words"}})

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.NotContains(t, rec.Body.String(), "two words")
	ok, err := c.keys.Has("openai")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Contains(t, c.get("/projects/"+id+"/messages"), `data-secret-form`, "the card is still open")
}

func TestSecret_ARequestWithoutTheCSRFTokenIsRefused(t *testing.T) {
	c := newClient(t)
	id := c.project()
	req := httptest.NewRequest(http.MethodPost, "/projects/"+id+"/messages/x/secret", strings.NewReader("secret=sk-nope"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	assert.Equal(t, http.StatusForbidden, c.do(req).Code)
}
