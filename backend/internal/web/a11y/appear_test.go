package a11y_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/errorbox"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modal"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentestinpaint"
)

// An error or result box that an HTMX swap puts in the page comes in with the fade of an alert (M5): it says data-appear, which static/src/css/motion.css fades in.
func TestBoxesOfASwap_FadeIn(t *testing.T) {
	boxes := map[string]string{
		"errorbox.Box":           render(t, errorbox.Box(errorbox.Props{View: apperr.New(apperr.CodeVRAM).View(apperr.LangJa), Lang: apperr.LangJa, ID: "r1"})),
		"modal.Error":            render(t, modal.Error("保存できませんでした")),
		"gentest.ConfirmFailed":  render(t, gentest.ConfirmFailed("見積りできません")),
		"gentest.ConfirmNote":    render(t, gentest.ConfirmNote("追加しました")),
		"gentestinpaint.Failed":  render(t, gentestinpaint.Failed("描き直せません")),
		"gentestinpaint.Started": render(t, gentestinpaint.Started("/generation-test/history")),
	}
	for name, html := range boxes {
		assert.Contains(t, html, " data-appear", name)
	}
}
