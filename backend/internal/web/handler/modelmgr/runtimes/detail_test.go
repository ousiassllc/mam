package runtimes_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/localruntime/lrtest"
	"github.com/ousiassllc/mam/backend/internal/web/screentest"
)

// A card that is drawn again by its poll keeps its id, and neither it nor the answers carry the dialog: the modal that is open is not touched by
// the swap (#954).
func TestRuntimes_ThePollingCardKeepsItsIdAndNeverCarriesTheModal(t *testing.T) {
	s := newRuntimeScreen(t)
	rec := s.Do(http.MethodPost, runtimePart(lrtest.ID, "install"), nil)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	for _, body := range []string{rec.Body.String(), s.Get(runtimePart(lrtest.ID)).Body.String()} {
		assert.Contains(t, body, `id="mm-runtime-gpt-sovits"`)
		assert.Contains(t, body, `hx-trigger="every 2s"`)
		assert.NotContains(t, body, "<dialog")
	}
	page := s.Page()
	assert.Equal(t, 1, strings.Count(page, `<dialog id="mam-modal"`))
	assert.Greater(t, strings.Index(page, `<dialog id="mam-modal"`), strings.Index(page, `id="mm-runtimes"`))
	s.waitRuntimeJob(screentest.JobID(t, rec.Body.String()))
}

func TestRuntimes_TheDetailOfARuntimeThatIsNotThereIsTheReasonInTheModal(t *testing.T) {
	s := newRuntimeScreen(t)
	rec := s.Get(runtimePart("nope", "detail"))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "対象が見つかりません")
	assert.Contains(t, rec.Body.String(), "data-modal-close")
}
