package modelpick_test

import (
	"context"
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
	assert.NotContains(t, html, "disabled")

	f.Disabled = true
	html = fieldHTML(t, f)
	assert.Equal(t, 3, strings.Count(html, "disabled"), "both inputs and the button")
}
