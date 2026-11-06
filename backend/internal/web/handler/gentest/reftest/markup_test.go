package reftest_test

import (
	"html"
	"net/http"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

var flare = string(domain.ModelGPTImage26Flare)

// #1096: the role "マークアップ" is on the select of an input image, offered only while a chosen model lists it (GPT Image 2.6 through the API key says so in
// ReferenceRoles), and the card says which roles the model takes.
func TestTheMarkupRoleIsOfferedOnlyToTheModelsThatTakeIt(t *testing.T) {
	s := newScreen(t, gentesttest.WithReferenceModels(loras(t).URL))
	ref := "&ref=" + s.historyRef()

	with := s.page(paths.GenerationTest() + "?tier=detail&model=" + flare + ref)
	assert.Contains(t, card(t, with, flare), "markup", "the card hands the role to the script")
	row := regexp.MustCompile(`(?s)<li[^>]*data-gt-ref.*?</li>`).FindString(with)
	option := regexp.MustCompile(`<option value="markup"([^>]*)>([^<]*)</option>`).FindStringSubmatch(row)
	require.Len(t, option, 3, "the markup role is on the select")
	assert.NotContains(t, option[1], "disabled", "a chosen model takes it")
	assert.Contains(t, option[2], "マークアップ（透明 PNG を 1 枚目に重ねる）")

	without := s.page(paths.GenerationTest() + "?tier=detail&model=" + string(gentesttest.EditModel) + ref)
	assert.NotContains(t, card(t, without, string(gentesttest.EditModel)), "markup")
	row = regexp.MustCompile(`(?s)<li[^>]*data-gt-ref.*?</li>`).FindString(without)
	option = regexp.MustCompile(`<option value="markup"([^>]*)>`).FindStringSubmatch(row)
	require.Len(t, option, 2)
	assert.Contains(t, option[1], "disabled", "a model that does not list it is not offered it")
}

// The role is accepted by the form (not turned into a plain reference), carried to the confirmation, and a markup image on a model that cannot take it is refused
// by the service with the reason, which the confirmation shows.
func TestAMarkupImageIsAcceptedCarriedAndRefusedWhereItCannotBeTaken(t *testing.T) {
	s := newScreen(t, gentesttest.WithReferenceModels(loras(t).URL))
	form := batchForm(flare)
	form["ref"] = []string{"upload::#1", "upload::#2"}
	form.Set("role_upload::#1", "edit")
	form.Set("role_upload::#2", "markup")
	base, markup := gentesttest.SolidPNG(8, 8, 255), gentesttest.SolidPNG(8, 8, 0)

	hidden := s.confirmed(form, base, markup)
	require.Len(t, hidden["ref"], 2)
	assert.Equal(t, "edit", hidden.Get("role_"+hidden["ref"][0]))
	assert.Equal(t, "markup", hidden.Get("role_"+hidden["ref"][1]), "the markup role is kept, not turned into a reference")

	other := batchForm(string(gentesttest.EditModel))
	other["ref"] = form["ref"]
	other.Set("role_upload::#1", "edit")
	other.Set("role_upload::#2", "markup")
	rec := s.estimate(other, base, markup)
	assert.GreaterOrEqual(t, rec.Code, http.StatusBadRequest, "a model that cannot take a markup image refuses it")
	assert.Contains(t, html.UnescapeString(rec.Body.String()), "対応していません")
}

// The browser posts the strength slider of every row, the row of a markup layer included (its value is the slider's default). That is not a strength the user
// asked for: the markup is confirmed, not refused with "マークアップに強さは指定できません" (that is for a caller, such as an MCP client, who names one).
func TestAMarkupRowThatPostsItsSliderIsConfirmed(t *testing.T) {
	s := newScreen(t, gentesttest.WithReferenceModels(loras(t).URL))
	form := batchForm(flare)
	form["ref"] = []string{"upload::#1", "upload::#2"}
	form.Set("role_upload::#1", "edit")
	form.Set("role_upload::#2", "markup")
	form.Set("strength_upload::#1", "0.7")
	form.Set("strength_upload::#2", "0.7")

	hidden := s.confirmed(form, gentesttest.SolidPNG(8, 8, 255), gentesttest.SolidPNG(8, 8, 0))

	assert.Equal(t, "markup", hidden.Get("role_"+hidden["ref"][1]))
	assert.Empty(t, hidden.Get("strength_"+hidden["ref"][1]), "a markup layer has no strength to carry")
}
