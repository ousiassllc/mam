package handler

import (
	"bufio"
	"bytes"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// uploadServer serves readMultipart behind a server whose ReadTimeout is far shorter than the upload
// takes. It answers with the status of readMultipart's outcome.
func uploadServer(t *testing.T, readTimeout time.Duration, limit int64) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/upload", func(c *gin.Context) {
		form, err := readMultipart(c, limit)
		if err != nil {
			status, _ := statusOf(c, err)
			c.Status(status)
			return
		}
		c.String(http.StatusOK, form.Value["name"][0])
	})
	srv := httptest.NewUnstartedServer(r)
	srv.Config.ReadTimeout = readTimeout
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

// slowUpload sends a multipart body in chunks with a pause between them and returns the status.
func slowUpload(t *testing.T, url string, chunks int, pause time.Duration) (int, error) {
	t.Helper()
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		defer func() { _ = pw.Close() }()
		// The server giving up closes the reading side: the writes then fail and the sender stops.
		time.Sleep(pause)
		if mw.WriteField("name", "bundle") != nil {
			return
		}
		fw, err := mw.CreateFormFile("file", "a.bin")
		if err != nil {
			return
		}
		for i := 0; i < chunks; i++ {
			time.Sleep(pause)
			if _, err := fw.Write(bytes.Repeat([]byte("x"), 1<<10)); err != nil {
				return
			}
		}
		_ = mw.Close()
	}()
	req, err := http.NewRequest(http.MethodPost, url+"/upload", pr)
	require.NoError(t, err)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode, nil
}

// A body that keeps arriving is never cut off by the server's absolute ReadTimeout (an 8 GiB bundle
// on a slow link takes longer than it), but one that stalls is (#325).
func TestReadMultipart_AnUploadIsHeldToAnIdleTimeNotAnAbsoluteOne(t *testing.T) {
	old := uploadIdleTimeout
	uploadIdleTimeout = 400 * time.Millisecond
	t.Cleanup(func() { uploadIdleTimeout = old })
	srv := uploadServer(t, 300*time.Millisecond, 1<<20)

	status, err := slowUpload(t, srv.URL, 8, 100*time.Millisecond) // 800 ms in all, 100 ms between reads
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)

	status, err = slowUpload(t, srv.URL, 2, 900*time.Millisecond) // stalls longer than the idle time
	if err == nil {
		assert.NotEqual(t, http.StatusOK, status)
	}
}

func TestReadMultipart_ADeclaredSizeOverTheLimitIsRefusedBeforeReadingIt(t *testing.T) {
	srv := uploadServer(t, time.Minute, 1<<10)
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	// Only the headers are sent: the answer must not wait for the 1 MiB the request declares.
	_, err = io.WriteString(conn, "POST /upload HTTP/1.1\r\nHost: x\r\nContent-Type: multipart/form-data; boundary=x\r\nContent-Length: 1048576\r\n\r\n")
	require.NoError(t, err)
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
}
