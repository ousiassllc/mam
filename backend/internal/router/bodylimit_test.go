package router_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/web/middleware"
)

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
