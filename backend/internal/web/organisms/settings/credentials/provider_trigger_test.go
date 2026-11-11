package credentials_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/credentials"
)

func providerPanel(t *testing.T) string {
	t.Helper()
	return renderPanel(t, credentials.Data{Provider: &credentials.ProviderData{Choices: []credentials.ProviderChoice{
		{Value: "official", Label: "公式", Checked: true}, {Value: "openrouter", Label: "OpenRouter"},
	}}})
}

// Moving through the radios with the arrow keys fires a change for each provider passed. This checks the markup that makes htmx save only once the choice has settled
// (a delay that each change restarts, and a request that replaces the one in flight); that htmx then waits is htmx's own behaviour, which Go cannot run.
func TestPanel_TheProviderFormCarriesTheDebouncedChangeTrigger(t *testing.T) {
	html := providerPanel(t)

	assert.Regexp(t, `<form[^>]*hx-put="[^"]*"[^>]*hx-trigger="change delay:600ms"[^>]*hx-sync="this:replace"[^>]*data-testid="api-provider-form"`, html,
		"the trigger and the sync are on the form that holds the radios")
}

// The save swaps the whole panel (outerHTML), so the radio that has the focus is replaced: htmx puts the focus back only on an element with the same id, hence each radio has a
// stable id of its own.
func TestPanel_EachProviderRadioHasAStableIDSoTheFocusSurvivesTheSwap(t *testing.T) {
	html := providerPanel(t)

	for _, value := range []string{"official", "openrouter"} {
		radio := regexp.MustCompile(`<input[^>]*id="api-provider-choice-` + value + `"[^>]*>`).FindString(html)
		require.NotEmpty(t, radio, value)
		assert.Contains(t, radio, `name="provider"`, value)
		assert.Contains(t, radio, `type="radio"`, value)
	}
}
