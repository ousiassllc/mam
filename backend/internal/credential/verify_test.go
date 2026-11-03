package credential_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential"
)

// forgetful is a store that takes a write and then does not return it, as the secure storage of another OS user does (#1006).
type forgetful struct{ credential.Store }

func (forgetful) Set(string, string) error   { return nil }
func (forgetful) Get(string) (string, error) { return "", credential.ErrNotFound }

// refusing is a store that cannot write.
type refusing struct{ forgetful }

func (refusing) Set(string, string) error { return errors.New("the vault is locked") }

// unreadable is a store that takes the write and cannot be read back.
type unreadable struct{ forgetful }

func (unreadable) Get(string) (string, error) { return "", errors.New("the vault is locked") }

// tooLarge is a store that refuses the size of the key.
type tooLarge struct{ forgetful }

func (tooLarge) Set(string, string) error { return credential.ErrTooLarge }

// A key that the store took but does not return is an error at the save, not a quiet success that shows as "not configured" in
// list_credentials later.
func TestSetChecked_FailsWhenTheStoreDoesNotReturnTheKey(t *testing.T) {
	err := credential.SetChecked(forgetful{}, "openai", "sk-secret")

	var store *credential.StoreError
	require.ErrorAs(t, err, &store)
	assert.Contains(t, store.Message, "読み戻せませんでした")
	assert.Contains(t, store.Message, "OS ユーザー", "the likely cause: the storage of another OS user")
	assert.NotContains(t, err.Error(), "sk-secret")
}

func TestSetChecked_FailsWhenTheStoreRefusesOrCannotBeRead(t *testing.T) {
	var store *credential.StoreError

	err := credential.SetChecked(refusing{}, "openai", "sk-secret")
	require.ErrorAs(t, err, &store)
	assert.Contains(t, store.Message, "保存できませんでした")
	assert.ErrorContains(t, err, "the vault is locked")

	err = credential.SetChecked(unreadable{}, "openai", "sk-secret")
	require.ErrorAs(t, err, &store)
	assert.Contains(t, store.Message, "確かめられませんでした")
}

func TestSetChecked_KeepsTheSizeRefusalForTheCallerToWord(t *testing.T) {
	assert.ErrorIs(t, credential.SetChecked(tooLarge{}, "openai", "k"), credential.ErrTooLarge)
}
