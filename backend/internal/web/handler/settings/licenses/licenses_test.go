package licenses_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/licenses"
	handler "github.com/ousiassllc/mam/backend/internal/web/handler/settings/licenses"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/licenses"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

const hostile = `<script>alert("x")</script> & <img src=x onerror=alert(1)>`

// A source of entries of any kind (here a second one beside the notices, as the weights of a local runtime would be) is
// a list of groups and entries on the page, with nothing in the screen changed; whatever its text holds is escaped.
func catalog() *licenses.Catalog {
	return licenses.New(
		func() []licenses.Entry {
			return []licenses.Entry{{Group: "A", Title: "first " + hostile, Body: "text of first " + hostile}}
		},
		func() []licenses.Entry {
			return []licenses.Entry{{Group: "B", Title: "second", Body: "text of second"}, {Group: "A", Title: "third", Body: "x"}}
		},
	)
}

func TestSectionData_GroupsAndTextPaths(t *testing.T) {
	d := handler.SectionData(catalog())
	require.Len(t, d.Groups, 2)
	assert.Equal(t, "A", d.Groups[0].Name)
	require.Len(t, d.Groups[0].Items, 2)
	assert.Equal(t, paths.LicenseText(d.Groups[0].Items[0].ID), d.Groups[0].Items[0].TextURL)
	assert.Equal(t, 3, d.Count())
}

func TestSection_ListsEntriesClosedWithTheirTextUnloadedAndEscapesTitles(t *testing.T) {
	var b bytes.Buffer
	require.NoError(t, view.Section(handler.SectionData(catalog())).Render(context.Background(), &b))
	html := b.String()

	assert.Equal(t, 3, bytes.Count(b.Bytes(), []byte(`data-testid="license-entry"`)))
	assert.Equal(t, 2, bytes.Count(b.Bytes(), []byte(`data-testid="license-group"`)))
	assert.NotContains(t, html, "text of first", "the text is read when the entry is opened")
	assert.NotContains(t, html, "<script>", "a title is escaped")
	assert.NotContains(t, html, "<img", "a title is escaped")
	assert.Contains(t, html, "&lt;script&gt;")
	assert.NotContains(t, html, " open", "every entry starts closed")
}

func TestText_AnswersWithTheEscapedTextOfOneEntry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cat := catalog()
	engine := gin.New()
	engine.GET(paths.LicenseText(":id"), handler.Text(cat, func(c *gin.Context) { c.String(http.StatusOK, "the page") }))
	get := func(target string, htmx bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		if htmx {
			req.Header.Set("HX-Request", "true")
		}
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}

	first := cat.Groups()[0].Sections[0]
	rec := get(paths.LicenseText(first.ID), true)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "text of first")
	assert.Contains(t, rec.Body.String(), "&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt; &amp; &lt;img src=x onerror=alert(1)&gt;")
	assert.NotContains(t, rec.Body.String(), "<script>")
	assert.NotContains(t, rec.Body.String(), "<img")

	assert.Equal(t, "the page", get(paths.LicenseText(first.ID), false).Body.String(), "not HTMX: the page of the category")
	assert.Equal(t, http.StatusNotFound, get(paths.LicenseText("999"), true).Code)
}
