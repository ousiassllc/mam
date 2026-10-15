package credential_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/ousiassllc/mam/backend/internal/credential"
)

func TestKeyring_SetGetDeleteRoundTrip(t *testing.T) {
	keyring.MockInit()
	store := credential.NewKeyring()

	has, err := store.Has("kling")
	require.NoError(t, err)
	assert.False(t, has)

	require.NoError(t, store.Set("kling", "sk-one"))
	require.NoError(t, store.Set("kling", "sk-two"), "Set replaces an existing key")
	require.NoError(t, store.Set("seedance", "sk-other"))

	got, err := store.Get("kling")
	require.NoError(t, err)
	assert.Equal(t, "sk-two", got)
	has, err = store.Has("kling")
	require.NoError(t, err)
	assert.True(t, has)

	require.NoError(t, store.Delete("kling"))
	_, err = store.Get("kling")
	assert.ErrorIs(t, err, credential.ErrNotFound)
	other, err := store.Get("seedance")
	require.NoError(t, err)
	assert.Equal(t, "sk-other", other, "providers are isolated")
}

func TestKeyring_NotFound(t *testing.T) {
	keyring.MockInit()
	store := credential.NewKeyring()
	_, err := store.Get("missing")
	assert.ErrorIs(t, err, credential.ErrNotFound)
	assert.ErrorIs(t, store.Delete("missing"), credential.ErrNotFound)
}

func TestKeyring_RejectsEmptyInput(t *testing.T) {
	keyring.MockInit()
	store := credential.NewKeyring()
	assert.Error(t, store.Set("", "key"))
	assert.Error(t, store.Set("kling", ""))
	_, err := store.Get(" ")
	assert.Error(t, err)
}

func TestKeyring_StorageFailureIsNotReportedAsMissing(t *testing.T) {
	boom := errors.New("secret service unavailable")
	keyring.MockInitWithError(boom)
	store := credential.NewKeyring()

	assert.ErrorIs(t, store.Set("kling", "k"), boom)
	_, err := store.Get("kling")
	assert.ErrorIs(t, err, boom)
	assert.NotErrorIs(t, err, credential.ErrNotFound)
	_, err = store.Has("kling")
	assert.ErrorIs(t, err, boom, "Has must surface storage errors instead of returning false")
}

func TestKeyring_SetMapsDataTooBigToErrTooLarge(t *testing.T) {
	keyring.MockInitWithError(keyring.ErrSetDataTooBig)
	err := credential.NewKeyring().Set("kling", "k")
	assert.ErrorIs(t, err, credential.ErrTooLarge)
}
