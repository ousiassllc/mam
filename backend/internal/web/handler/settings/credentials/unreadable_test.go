package credentials_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A key store that cannot be read leaves the cards without a state, but the cards that the choice of the API provider shows are the same as when it can be read
// (spec/components/settings-screen-categories.md §1.0): the aggregators' cards are not there under the official provider.
func TestPanel_AnUnreadableKeyStoreStillFollowsTheChoiceOfTheProvider(t *testing.T) {
	r := newProviderRig(t, false)
	r.keys.FailWith(errors.New("no secret service"))

	official := providers(r.panel())

	assert.Contains(t, official, "kling")
	assert.NotContains(t, official, "openrouter")
	assert.NotContains(t, official, "fal")
	assert.NotContains(t, official, "replicate")

	require.NoError(t, r.models.SaveVideoProvider("openrouter"))

	chosen := providers(r.panel())

	require.NotEmpty(t, chosen)
	assert.Equal(t, "openrouter", chosen[0], "the key of the chosen provider comes first")
	assert.NotContains(t, chosen, "kling")
	assert.NotContains(t, chosen, "kling4")
	assert.NotContains(t, chosen, "fal")
}
