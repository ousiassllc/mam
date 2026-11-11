package apikey_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/apikey"
)

func TestProvider_AFailedKeyLookupAfterTheSaveStillTellsTheSaveHappened(t *testing.T) {
	saved := ""
	h := apikey.NewProvider(apikey.ProviderDeps{
		View:       func() (apikey.ProviderView, error) { return apikey.ProviderView{Selected: "official"}, nil },
		Save:       func(p string) error { saved = p; return nil },
		Configured: func(string) (bool, error) { return false, errors.New("keychain locked") },
	})

	res, err := h.Apply(context.Background(), domain.Project{}, providerOp(t, "openrouter"))

	require.NoError(t, err, "the provider is saved: the success is not turned into an error")
	assert.Equal(t, "openrouter", saved)
	assert.Contains(t, res.Text, "OpenRouter")
	assert.Contains(t, res.Text, "にしました")
	assert.Contains(t, res.Text, "確かめられませんでした")
	assert.Empty(t, res.Messages)
}
