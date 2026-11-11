package chatscreen

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/chat"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/chat"
)

// The batch of a result message was queued, so its block cannot just vanish when it cannot be read: the thread gets a failure line that says where the results are.
func TestAResultMessageWhoseBatchCannotBeReadBecomesAFailureLine(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	rig := gentesttest.New(t)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/", nil)

	v := Handlers{Gentest: rig.Svc}.messageView(c, "s", chat.Message{ID: "m1", Kind: chat.KindResult, BatchID: "no-such-batch"})

	assert.Equal(t, view.KindStatus, v.Kind)
	assert.True(t, v.Failed)
	assert.Contains(t, v.Text, "履歴")
	assert.Nil(t, v.Result)
}
