package router_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/middleware"
)

// countReader counts bytes a handler pulls from the body.
type countReader struct {
	r io.Reader
	n int
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

func TestBodyLimit_OversizedJSONAndFormAre413(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液の縦型15秒CMを作りたい")
	filler := strings.Repeat("a", middleware.MaxBodyBytes)

	rec := a.request(http.MethodPut, "/edit-timelines/tl1", []byte(`{"clips":[],"pad":"`+filler+`"}`), "application/json", false)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "送信内容が大きすぎます")

	// A urlencoded form over the limit is refused, not read as an empty form.
	rec = a.request(http.MethodPost, "/projects/"+id+"/edit/rough-cut", []byte("branch_id="+filler), "application/x-www-form-urlencoded", false)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
}

// A multipart Content-Type does not lift the cap on routes that do not upload. The body is refused
// before it is parsed, so it is neither buffered nor spilled to a temp file.
func TestBodyLimit_OversizedMultipartOnNonUploadRoutesIs413Unread(t *testing.T) {
	a := newSerialApp(t)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	payload := bytes.Repeat([]byte("a"), middleware.MaxBodyBytes+1)

	for _, target := range []struct{ method, path string }{
		{http.MethodPut, "/edit-timelines/tl1"},
		{http.MethodPost, "/shots/shot1/branch"},
	} {
		body := &countReader{r: bytes.NewReader(payload)}
		req := httptest.NewRequest(target.method, target.path, body)
		req.ContentLength = int64(len(payload))
		req.Header.Set("Content-Type", "multipart/form-data; boundary=----mam")
		req.AddCookie(a.cookie)
		req.Header.Set(middleware.CSRFHeaderName, a.token)
		rec := do(a.engine, req)
		assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, "%s %s: %s", target.method, target.path, rec.Body.String())
		assert.Zero(t, body.n, "%s %s read the body", target.method, target.path)
	}
	entries, err := os.ReadDir(tmp)
	require.NoError(t, err)
	assert.Empty(t, entries, "the oversized body was not spilled to a temp file")
}

// An upload route is exempt from the cap only for the multipart body its handler limits itself: a
// urlencoded body on POST /projects/:id/messages is capped like any other route.
func TestBodyLimit_OversizedUrlencodedOnUploadRouteIs413Unread(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液の縦型15秒CMを作りたい")
	payload := []byte("content=" + strings.Repeat("a", middleware.MaxBodyBytes))

	body := &countReader{r: bytes.NewReader(payload)}
	req := httptest.NewRequest(http.MethodPost, "/projects/"+id+"/messages", body)
	req.ContentLength = int64(len(payload))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(a.cookie)
	req.Header.Set(middleware.CSRFHeaderName, a.token)
	rec := do(a.engine, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
	assert.Zero(t, body.n, "the body was read")

	// Chunked (unknown length): the limit still stops the read.
	req = httptest.NewRequest(http.MethodPost, "/projects/"+id+"/messages", io.NopCloser(bytes.NewReader(payload)))
	req.ContentLength = -1
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(a.cookie)
	req.Header.Set(middleware.CSRFHeaderName, a.token)
	rec = do(a.engine, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
}
