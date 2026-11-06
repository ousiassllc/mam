package ollama_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/service/ollamapull/ollamatest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
	"github.com/ousiassllc/mam/backend/internal/web/screentest"
)

const gemma = "gemma4:12b"

func pullPath() string { return paths.ModelManagerPart("ollama", "models", "pull") }

func sectionPath(jobs ...string) string {
	u := paths.ModelManagerPart("ollama")
	if len(jobs) > 0 {
		u += "?jobs=" + jobs[0]
	}
	return u
}

func deletePath(name string) string {
	return paths.ModelManagerPart("ollama", "models") + "?name=" + url.QueryEscape(name)
}

func detailPath(name string) string {
	return paths.ModelManagerPart("ollama", "models", "detail") + "?name=" + url.QueryEscape(name)
}

// pull asks for the model through the screen.
func pull(s *screentest.Screen, name string) *httptest.ResponseRecorder {
	return s.Do(http.MethodPost, pullPath(), url.Values{"name": {name}})
}

// successLines is the stream of a pull of the model that works: every layer from 0 to its size, then the manifest.
func successLines(t *testing.T, name string) []ollamatest.Line {
	t.Helper()
	for _, m := range catalog.Default().OllamaModels {
		if m.Name != name {
			continue
		}
		l := m.ModelLayer()
		return []ollamatest.Line{
			{"status": "pulling manifest"},
			ollamatest.Status("pulling x", l.Digest, l.Size, 0),
			ollamatest.Status("pulling x", l.Digest, l.Size, l.Size/2),
			ollamatest.Status("pulling x", l.Digest, l.Size, l.Size),
			{"status": "verifying sha256 digest"},
			{"status": "writing manifest"},
			{"status": "success"},
		}
	}
	require.FailNow(t, "no such model", name)
	return nil
}

// stateOf is the data-state of the card of the model in body.
func stateOf(t *testing.T, body, name string) string {
	t.Helper()
	m := regexp.MustCompile(`data-model="` + regexp.QuoteMeta(name) + `" data-state="([a-z]+)"`).FindStringSubmatch(body)
	require.Len(t, m, 2, "no card of %s in %s", name, body)
	return m[1]
}

// percentOf is the value of the progress bar of the section.
func percentOf(t *testing.T, body string) int {
	t.Helper()
	m := regexp.MustCompile(`<progress[^>]*value="(\d+)"`).FindStringSubmatch(body)
	require.Len(t, m, 2, "no progress bar in %s", body)
	n, err := strconv.Atoi(m[1])
	require.NoError(t, err)
	return n
}

// disabledPulls is how many pull buttons of body are off.
func disabledPulls(body string) int {
	n := 0
	for _, b := range regexp.MustCompile(`<button[^>]*data-testid="mm-ollama-pull"[^>]*>`).FindAllString(body, -1) {
		if disabledAttr.MatchString(b) {
			n++
		}
	}
	return n
}

// disabledAttr is the attribute disabled (the class of a button has "disabled:" variants).
var disabledAttr = regexp.MustCompile(`\sdisabled[\s>]`)
