package brandtemplate_test

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/brandtemplate"
)

func renderHTML(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, c.Render(context.Background(), &sb))
	return sb.String()
}

func data() brandtemplate.Data {
	return brandtemplate.Data{ProjectID: "p1", Templates: []domain.BrandTemplate{{ID: "t1", Name: "T"}}}
}

// Removing a template replaces the list only: the save form beside it keeps the name typed into it (#729).
// Only saving carries a fresh form, out of band.
func TestItems_DoNotCarryTheSaveForm(t *testing.T) {
	list := renderHTML(t, brandtemplate.Items(data()))
	saved := renderHTML(t, brandtemplate.Saved(data()))

	assert.Contains(t, list, `id="brand-template-list"`)
	assert.NotContains(t, list, `id="brand-template-form"`)
	assert.NotContains(t, list, `name="name"`)
	assert.Contains(t, list, `hx-target="#brand-template-list"`, "the remove button answers into the list")
	assert.Contains(t, saved, `id="brand-template-form"`)
	assert.Contains(t, saved, `hx-swap-oob="true"`)
}

func TestPanel_CarriesTheSaveFormOutsideTheListThatIsReplaced(t *testing.T) {
	html := renderHTML(t, brandtemplate.Panel(data()))

	assert.Contains(t, html, `id="brand-template-list"`)
	assert.Contains(t, html, `id="brand-template-form"`)
	assert.Contains(t, html, `id="brand-template-name"`)
	assert.Contains(t, html, `hx-target="#brand-template-list"`, "the save form answers into the list")
	assert.NotContains(t, html, "hx-swap-oob")
}

func TestSaved_KeepsTheInputAndTheErrorOfARefusedName(t *testing.T) {
	d := data()
	d.Typed = map[string]string{"name": "dup"}
	d.Errors = map[string]string{"name": "すでにあります"}
	html := renderHTML(t, brandtemplate.Saved(d))

	assert.Contains(t, html, `value="dup"`)
	assert.Contains(t, html, "すでにあります")
}
