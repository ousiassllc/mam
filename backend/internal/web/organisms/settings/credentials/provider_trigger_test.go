package credentials_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/credentials"
)

// Moving through the radios with the arrow keys fires a change for each provider passed: the choice is saved when it has settled (a delay that each change
// restarts), not for every provider on the way.
func TestPanel_TheProviderIsSavedWhenTheChoiceSettlesNotOnEveryArrowKey(t *testing.T) {
	html := renderPanel(t, credentials.Data{Provider: &credentials.ProviderData{Choices: []credentials.ProviderChoice{
		{Value: "official", Label: "公式", Checked: true}, {Value: "openrouter", Label: "OpenRouter"},
	}}})

	assert.Contains(t, html, `hx-trigger="change delay:600ms"`)
	assert.Contains(t, html, `hx-sync="this:replace"`)
}
