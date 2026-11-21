package modelpick_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"
)

func fieldHTML(t *testing.T, f modelpick.FieldData) string {
	var sb strings.Builder
	require.NoError(t, modelpick.Field(f).Render(context.Background(), &sb))
	return sb.String()
}

// An input of the field that has no name is not posted (it holds what a script reads of the choice); a disabled field posts nothing and cannot be opened.
func TestFieldInputsWithoutANameAreNotPostedAndADisabledFieldIsClosed(t *testing.T) {
	f := modelpick.FieldData{ID: "f", Label: "LoRA", URL: "/model-picker/lora", Inputs: []modelpick.Input{{Name: "lora", Key: "value", Value: "a"}, {Key: "known", Value: "true"}}}
	html := fieldHTML(t, f)
	assert.Contains(t, html, `<input type="hidden" name="lora" value="a" data-mp-from="value">`)
	assert.Contains(t, html, `<input type="hidden" value="true" data-mp-from="known">`, "no name attribute at all")
	assert.NotRegexp(t, `\sdisabled[\s>=]`, html, "the disabled: classes of the button are not the attribute")

	f.Disabled = true
	html = fieldHTML(t, f)
	assert.Len(t, regexp.MustCompile(`\sdisabled[\s>=]`).FindAllString(html, -1), 3, "both inputs and the button")
	assert.Contains(t, html, "disabled:opacity-40", "it looks closed (O7)")
	assert.Contains(t, html, "disabled:cursor-not-allowed", "and it is not the pointer over it")
}

// The button that opens the picker is pressed like any button: it starts the ripple (the UI guide, M4).
func TestFieldOpenButtonIsMarkedForTheRipple(t *testing.T) {
	html := fieldHTML(t, modelpick.FieldData{ID: "f", Label: "LoRA", URL: "/model-picker/lora"})
	assert.Equal(t, 1, strings.Count(html, "data-ripple"))
}
