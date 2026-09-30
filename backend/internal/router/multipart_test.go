package router_test

import (
	"net/http"
	"net/url"
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
