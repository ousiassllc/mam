package chatcard_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/lorafile"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/handler"
)

// statusOfDeclared is the status a handler gives a multipart request that announces `declared` bytes (the body itself is empty: the announced length is checked before a
// byte is read; a request that passes the check fails to parse as 400). The service is not reached in these cases, so none is given.
func statusOfDeclared(t *testing.T, h gin.HandlerFunc, declared int64) int {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/projects/p/messages", strings.NewReader(""))
	c.Request.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	c.Request.ContentLength = declared
	h(c)
	return w.Code
}

// A message of the chat may carry a LoRA (up to lorafile.MaxBytes) beside the attachments of the Brief, and the request has a limit of its own (#1140).
func TestMessageRequestLimit_AChatMessageTakesALoRA(t *testing.T) {
	loraRequest := lorafile.MaxBytes + 1<<20
	post := handler.PostMessage((*service.ChatService)(nil))

	assert.Equal(t, http.StatusBadRequest, statusOfDeclared(t, post, loraRequest), "within the limit of a message: read (and here malformed)")
	assert.Equal(t, http.StatusRequestEntityTooLarge, statusOfDeclared(t, post, 2*lorafile.MaxBytes), "over the limit of a message")
}
