package gtshare_test

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/apperr"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gtshare"
)

func ptr(v float64) *float64 { return &v }

// The history, the form and the prompt match name a unit price the same way: one function, the longer words of the two.
func TestPriceText(t *testing.T) {
	cases := map[string]struct {
		price modelcatalog.Price
		want  string
	}{
		"free":                  {modelcatalog.Price{Basis: modelcatalog.BasisFree}, "無料（ローカル）"},
		"per image":             {modelcatalog.Price{Basis: modelcatalog.BasisPriced, PerImageUSD: ptr(0.03)}, "$0.0300 / 枚"},
		"per second, in order":  {modelcatalog.Price{Basis: modelcatalog.BasisPriced, PerSecondUSD: map[string]float64{"1080p": 0.2, "720p": 0.1}}, "1080p $0.2000 / 秒・720p $0.1000 / 秒"},
		"no price":              {modelcatalog.Price{Basis: modelcatalog.BasisUnknown}, "単価なし（見積りの合計に入りません）"},
		"priced but no figures": {modelcatalog.Price{Basis: modelcatalog.BasisPriced}, ""},
	}
	for name, tc := range cases {
		assert.Equal(t, tc.want, gtshare.PriceText(tc.price), name)
	}
}

func TestUSDAndStatusText(t *testing.T) {
	assert.Equal(t, "$0.0300", gtshare.USD(0.03))
	assert.Equal(t, "—", gtshare.USDPtr(nil))
	assert.Equal(t, "$1.5000", gtshare.USDPtr(ptr(1.5)))
	for _, s := range []string{gentest.StatusQueued, gentest.StatusRunning, gentest.StatusSucceeded, gentest.StatusFailed, gentest.StatusCancelled} {
		assert.NotEmpty(t, gtshare.StatusText[s], s)
	}
}

func TestConfirmLines(t *testing.T) {
	lines := gtshare.ConfirmLines([]gentest.ModelEstimate{
		{Label: "local", Items: 2, Basis: modelcatalog.BasisFree},
		{Label: "cloud", Items: 3, Basis: modelcatalog.BasisPriced, UnitUSD: ptr(0.1), SubtotalUSD: ptr(0.3)},
		{Label: "llm", Items: 1, Basis: modelcatalog.BasisUnknown},
	})
	assert.Equal(t, "無料（ローカル）", lines[0].Unit)
	assert.Equal(t, "$0.0000", lines[0].Subtotal)
	assert.Equal(t, "$0.1000", lines[1].Unit)
	assert.Equal(t, "$0.3000", lines[1].Subtotal)
	assert.Equal(t, "単価なし", lines[2].Unit)
	assert.Equal(t, "合計に入りません", lines[2].Subtotal)
	assert.Equal(t, 3, lines[1].Items)
}

func TestRefuse(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	for name, tc := range map[string]struct {
		err    error
		status int
	}{
		"invalid":     {gterr.Invalid("だめ"), http.StatusUnprocessableEntity},
		"not found":   {gterr.ErrNotFound, http.StatusNotFound},
		"unavailable": {gterr.ErrUnavailable, http.StatusServiceUnavailable},
		"too big":     {&http.MaxBytesError{Limit: 1}, http.StatusRequestEntityTooLarge},
		"a fault":     {errors.New("disk"), http.StatusInternalServerError},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		status, msg := gtshare.Refuse(c, tc.err)
		assert.Equal(t, tc.status, status, name)
		assert.NotEmpty(t, msg, name)
		assert.NotContains(t, msg, "disk", name+": the cause stays out of the page")
	}
}

// The line a part of a screen shows for a refusal (the face swap box, the picker of the exposed values, the hint of the easy tier) is the common
// error (#1010): what the service worded stays, and a failure nobody knows is told as unknown, with the start of the original message as the
// detail and an error ID that the log line shares, never as the text of the error alone.
func TestProblem(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	assert.Equal(t, "グラフを読めません", gtshare.Problem(logger, "x", fmt.Errorf("wrap: %w", gterr.Invalid("グラフを読めません"))), "a sentence the service worded")
	sentence := gtshare.Problem(logger, "x", fmt.Errorf("wrap: %w", apperr.New(apperr.CodeFaceFusionNone)))
	assert.Contains(t, sentence, "FaceFusion", "the cause and the remedy of the common error")
	assert.Empty(t, logs.String(), "a failure that is worded is not logged here")

	got := gtshare.Problem(logger, "the part could not tell why", errors.New("pq: connection reset by peer"))
	assert.Contains(t, got, "原因がわかりませんでした")
	assert.Contains(t, got, "詳細: pq: connection reset by peer", "the original message is kept as the detail")
	assert.NotEqual(t, "pq: connection reset by peer", got)
	assert.Regexp(t, `エラー ID: err_`, got)
	require.Contains(t, logs.String(), "the part could not tell why")
	assert.Contains(t, logs.String(), "error_id=err_", "the log line has the ID the sentence tells")
}
