package router_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service"
)

func TestMultipart_OversizedUploadIs413AndMalformedIs400(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液の縦型15秒CMを作りたい")
	path := "/projects/" + id + "/reviews/feedback"

	tooBig := make([]byte, service.MaxReviewResponseRequestBytes+1)
	rec := a.importReviewFile(path, "response", "response.json", tooBig)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
	assert.Equal(t, "none", rec.Header().Get("HX-Reswap"))
	assert.Contains(t, rec.Body.String(), "送信内容が大きすぎます")

	// Not a multipart body at all: a 400 that is not mistaken for a validation error.
	rec = a.form(http.MethodPost, path, url.Values{"response": {"x"}})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "送信内容の形式が正しくありません")

	// A file inside the limit still reaches the service (which rejects it as a response).
	rec = a.importReviewFile(path, "response", "response.json", []byte("{}"))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
}

func TestChat_MultipartMediaTypeIsCaseInsensitive(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液の縦型15秒CMを作りたい")
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("attachments", "ref.png")
	require.NoError(t, err)
	_, err = part.Write(pngBytes)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	contentType := strings.Replace(w.FormDataContentType(), "multipart/form-data", "Multipart/Form-Data", 1)
	require.Contains(t, contentType, "Multipart/Form-Data")

	// An attachment alone is a valid message: the file must reach the service.
	rec := a.request(http.MethodPost, "/projects/"+id+"/messages", buf.Bytes(), contentType, true)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var assets int
	require.NoError(t, a.db.Reader().QueryRow(`SELECT COUNT(*) FROM asset WHERE project_id = ?`, id).Scan(&assets))
	assert.Equal(t, 1, assets)
	// A body that is not multipart goes through readMultipart's error handling.
	rec = a.request(http.MethodPost, "/projects/"+id+"/messages", []byte("content=x"), "Multipart/Form-Data; boundary=mam", true)
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}
