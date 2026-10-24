package tokenstore_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/tokenstore"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
)

// windowsLimit is the size one Windows Credential Manager entry may have.
const windowsLimit = 2560

// keyring is a fake OS store that enforces the Windows limit and counts its entries.
func keyring() *credentialtest.Memory {
	m := credentialtest.New()
	m.LimitBytes(windowsLimit)
	return m
}

func jwtLike(n int, seed string) string {
	return seed + strings.Repeat("A", n-len(seed))
}

func sample(access, id string) tokenstore.Tokens {
	return tokenstore.Tokens{
		Access: access, Refresh: jwtLike(211, "rt.refresh"), IDToken: id,
		Meta: tokenstore.Meta{
			ClientID: "oaiapp_x", ExpiresAt: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC),
			Scopes: []string{"openid", "email"}, Email: "me@example.com", Plan: "plus",
		},
	}
}

func TestStore_RoundTripWithTokensAboveTheOSLimit(t *testing.T) {
	kv := keyring()
	s := tokenstore.New(kv, "official")
	// The measured Codex sizes (id 1704, access 1657, refresh 211) sum to 3572 > 2560; the SIWC access token may be longer than any entry.
	want := sample(jwtLike(1657, "eyJaccess"), jwtLike(5300, "eyJid"))

	require.NoError(t, s.Save(want), "no single entry may exceed the OS limit, so the store splits the long ones")
	got, err := s.Load()
	require.NoError(t, err)
	assert.Equal(t, want, got)

	long := sample(jwtLike(9000, "eyJaccess"), jwtLike(5300, "eyJid"))
	require.NoError(t, s.Save(long))
	got, err = s.Load()
	require.NoError(t, err)
	assert.Equal(t, long, got)
}

func TestStore_ShrinkingAFieldDeletesTheOrphanedPieces(t *testing.T) {
	kv := keyring()
	s := tokenstore.New(kv, "official")
	require.NoError(t, s.Save(sample(jwtLike(9000, "a"), "")))
	for _, key := range []string{"chatgpt/official/access", "chatgpt/official/access/0", "chatgpt/official/access/4"} {
		ok, err := kv.Has(key)
		require.NoError(t, err)
		assert.True(t, ok, key)
	}

	require.NoError(t, s.Save(sample(jwtLike(300, "a"), "")))
	for _, key := range []string{"chatgpt/official/access/0", "chatgpt/official/access/4", "chatgpt/official/id_token"} {
		ok, err := kv.Has(key)
		require.NoError(t, err)
		assert.False(t, ok, "%s must be gone", key)
	}
	got, err := s.Load()
	require.NoError(t, err)
	assert.Equal(t, jwtLike(300, "a"), got.Access)
	assert.Empty(t, got.IDToken)
}

func TestStore_ClearKeepsTheRegistrationAndLoadSaysNotSignedIn(t *testing.T) {
	kv := keyring()
	s := tokenstore.New(kv, "official")
	_, err := s.Load()
	require.ErrorIs(t, err, tokenstore.ErrNotSignedIn)

	require.NoError(t, s.SaveRegistration(tokenstore.Registration{HostID: "urn:uuid:1", ClientID: "oaiapp_x"}))
	require.NoError(t, s.Save(sample(jwtLike(9000, "a"), jwtLike(3000, "i"))))
	require.NoError(t, s.Clear())
	require.NoError(t, s.Clear(), "clearing twice is harmless")

	_, err = s.Load()
	require.ErrorIs(t, err, tokenstore.ErrNotSignedIn)
	reg, err := s.Registration()
	require.NoError(t, err)
	assert.Equal(t, tokenstore.Registration{HostID: "urn:uuid:1", ClientID: "oaiapp_x"}, reg)
	for _, key := range []string{"access", "access/0", "id_token", "id_token/0", "refresh", "meta"} {
		ok, err := kv.Has("chatgpt/official/" + key)
		require.NoError(t, err)
		assert.False(t, ok, key)
	}
}

func TestStore_ASplitEntryWithAMissingPieceIsNotSignedInAndNamespacesAreSeparate(t *testing.T) {
	kv := keyring()
	official, codex := tokenstore.New(kv, "official"), tokenstore.New(kv, "codex")
	require.NoError(t, official.Save(sample(jwtLike(9000, "a"), "")))
	require.NoError(t, kv.Delete("chatgpt/official/access/2"))

	_, err := official.Load()
	require.ErrorIs(t, err, tokenstore.ErrNotSignedIn, "a torn write is a sign-in to repeat, not a half token")
	_, err = codex.Load()
	require.ErrorIs(t, err, tokenstore.ErrNotSignedIn)
}

func TestStore_RefusesAFieldOverTheSanityLimitAndAMissingToken(t *testing.T) {
	s := tokenstore.New(keyring(), "official")
	err := s.Save(sample(jwtLike(tokenstore.MaxFieldBytes+1, "a"), ""))
	require.ErrorIs(t, err, tokenstore.ErrFieldTooLarge)
	assert.NotContains(t, err.Error(), "AAAA", "an error never carries a token")
	require.Error(t, s.Save(tokenstore.Tokens{Access: "a"}))
}

func TestStore_SaveMetaChangesOnlyTheMetadata(t *testing.T) {
	s := tokenstore.New(keyring(), "codex")
	tok := sample(jwtLike(3000, "a"), "")
	require.NoError(t, s.Save(tok))
	meta := tok.Meta
	meta.ReloginRequired, meta.ReloginReason = true, "refresh_token_expired"
	require.NoError(t, s.SaveMeta(meta))
	got, err := s.Load()
	require.NoError(t, err)
	assert.Equal(t, tok.Access, got.Access)
	assert.True(t, got.Meta.ReloginRequired)
}

func TestStore_AStoreThatFailsIsAnErrorNotSignedOut(t *testing.T) {
	kv := keyring()
	s := tokenstore.New(kv, "official")
	require.NoError(t, s.Save(sample("a", "")))
	kv.FailWith(credential.ErrUnavailable)
	_, err := s.Load()
	require.ErrorIs(t, err, credential.ErrUnavailable)
	assert.NotErrorIs(t, err, tokenstore.ErrNotSignedIn)
}
