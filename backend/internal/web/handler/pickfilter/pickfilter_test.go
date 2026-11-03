package pickfilter_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/web/handler/pickfilter"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.ReleaseMode)
	m.Run()
}

var screen = pickfilter.Screen{Key: "test-screen", Start: modelfilter.Default()}

const cookieName = pickfilter.CookiePrefix + "test-screen"

// request is a context for a GET of target that carries the cookies; the recorder is what the handler wrote.
func request(target string, cookies ...*http.Cookie) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	for _, k := range cookies {
		c.Request.AddCookie(k)
	}
	return c, w
}

// remember stores the condition by a request that states it (target) and returns the cookie the browser would keep.
func remember(t *testing.T, target string, cond modelfilter.Condition) *http.Cookie {
	t.Helper()
	c, w := request(target)
	screen.Remember(c, cond)
	for _, k := range w.Result().Cookies() {
		if k.Name == cookieName {
			return k
		}
	}
	return nil
}

func readOf(t *testing.T, target string, cookies ...*http.Cookie) modelfilter.Condition {
	t.Helper()
	c, _ := request(target, cookies...)
	cond, problem := screen.Read(c)
	require.Empty(t, problem)
	return cond
}

func TestRememberedConditionIsReadBackWhenTheScreenIsOpenedWithNone(t *testing.T) {
	cond := modelfilter.Condition{Kinds: []modelfilter.Kind{modelfilter.KindVideo}, Commercial: "ok", AvailableOnly: true, Text: "flux 2"}
	cookie := remember(t, "/screen?mf=1", cond)
	require.NotNil(t, cookie)

	assert.Equal(t, cond.Normalize(), readOf(t, "/screen", cookie), "the screen opened with no condition starts where it was left")
	assert.Equal(t, modelfilter.Condition{Kinds: []modelfilter.Kind{modelfilter.KindImage}}.Normalize(), readOf(t, "/screen?kinds=image", cookie),
		"a request that states a key of the condition is taken as it is, not merged with the remembered one")
}

func TestRememberKeepsShowEverythingAndTheStartIsTheMarkerOnly(t *testing.T) {
	everything := remember(t, "/screen?mf=1", modelfilter.Condition{}) // available_only=0 against a start that is on
	require.NotNil(t, everything)
	assert.Contains(t, everything.Value, "available_only", "'show everything' is written, or the start would come back")
	assert.False(t, readOf(t, "/screen", everything).AvailableOnly, "available_only=0 survives the round trip")

	atStart := remember(t, "/screen?mf=1", modelfilter.Default())
	require.NotNil(t, atStart)
	assert.True(t, readOf(t, "/screen", atStart).AvailableOnly)

	zeroStart := pickfilter.Screen{Key: "test-screen"} // a start with nothing: the condition at its start encodes to nothing, the marker is the value
	c, w := request("/screen?mf=1")
	zeroStart.Remember(c, modelfilter.Condition{})
	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, url.QueryEscape("mf=1"), cookies[0].Value, "an empty value would delete the cookie")
	assert.Equal(t, 365*24*60*60, cookies[0].MaxAge)
}

func TestRememberStoresNothingForARequestThatDidNotStateTheCondition(t *testing.T) {
	for _, target := range []string{"/screen", "/screen?kinds=image", "/screen?available_only=0", "/screen?mf_sync=1"} {
		c, w := request(target)
		screen.Remember(c, modelfilter.Condition{Kinds: []modelfilter.Kind{modelfilter.KindImage}})
		assert.Empty(t, w.Result().Cookies(), "%s has no marker (mf=1)", target)
		assert.Empty(t, w.Header().Values("Set-Cookie"), target)
	}
}

func TestRememberedCookieAttributes(t *testing.T) {
	cookie := remember(t, "/screen?mf=1", modelfilter.Condition{Kinds: []modelfilter.Kind{modelfilter.KindImage}, AvailableOnly: true})
	require.NotNil(t, cookie)
	assert.Equal(t, "mam_mf_test-screen", cookie.Name, "one cookie per screen: the prefix and the key")
	assert.True(t, cookie.HttpOnly, "the script does not read it")
	assert.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
	assert.Equal(t, "/", cookie.Path, "the same choice on every address of the screen")
	assert.Equal(t, 365*24*60*60, cookie.MaxAge)
}

func TestACookieThatDoesNotReadFallsBackToTheStart(t *testing.T) {
	start := screen.Start.Normalize()
	for name, value := range map[string]string{
		"an unknown word of a key":       url.QueryEscape("kinds=bogus"),
		"an unknown word of another key": url.QueryEscape("commercial=maybe&available_only=0"),
		"a broken escape":                "%zz",
		"a broken query string":          url.QueryEscape("kinds=%zz"),
		"not a query string":             url.QueryEscape("garbage"),
		"empty":                          "",
	} {
		assert.Equal(t, start, readOf(t, "/screen", &http.Cookie{Name: cookieName, Value: value}), name)
	}
	assert.Equal(t, start, readOf(t, "/screen"), "no cookie: the start")
	assert.Equal(t, start, readOf(t, "/screen", &http.Cookie{Name: pickfilter.CookiePrefix + "other-screen", Value: url.QueryEscape("kinds=video")}),
		"the cookie of another screen is not this screen's")
}

func TestAStatedConditionThatDoesNotReadFallsBackToTheStartAndSaysWhy(t *testing.T) {
	c, _ := request("/screen?mf=1&kinds=bogus")
	cond, problem := screen.Read(c)
	assert.Equal(t, screen.Start.Normalize(), cond)
	assert.Contains(t, problem, "bogus", "the bar shows the reason")
}
