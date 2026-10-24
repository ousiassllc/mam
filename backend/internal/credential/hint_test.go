package credential_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
)

func TestHint_RevealsOnlyTheEndOfAKey(t *testing.T) {
	for name, tc := range map[string]struct{ key, want string }{
		"long key":          {"sk-ant-abcdefghijklmnop1234", "1234"},
		"exactly 16":        {"0123456789abcdef", "cdef"},
		"short key":         {"abcdefgh", "gh"},
		"too short to hint": {"abc", ""},
	} {
		store := credentialtest.New()
		require.NoError(t, store.Set("p", tc.key))
		got, err := credential.Hint(store, "p")
		require.NoError(t, err, name)
		assert.Equal(t, tc.want, got, name)
		assert.Less(t, len(got), len(tc.key), name)
	}
}

func TestHint_IsEmptyWhenNoKeyIsStoredAndPassesStoreErrors(t *testing.T) {
	got, err := credential.Hint(credentialtest.New(), "none")
	require.NoError(t, err)
	assert.Empty(t, got)

	boom := errors.New("locked")
	_, err = credential.Hint(failingStore{credentialtest.New(), boom}, "p")
	assert.ErrorIs(t, err, boom)
}

type failingStore struct {
	credential.Store
	err error
}

func (f failingStore) Get(string) (string, error) { return "", f.err }
