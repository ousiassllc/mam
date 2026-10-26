package mcptoken

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/repository/mcptoken"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newService(t *testing.T) (*Service, *clock, func(string) string) {
	t.Helper()
	db := testdb.Open(t)
	s := New(mcptoken.New(db))
	c := &clock{t: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)}
	s.now = c.now
	column := func(q string) string {
		var v string
		require.NoError(t, db.Reader().QueryRow(q).Scan(&v))
		return v
	}
	return s, c, column
}

func TestIssue_StoresOnlyTheHashOfTheValue(t *testing.T) {
	ctx := context.Background()
	s, _, column := newService(t)

	issued, err := s.Issue(ctx, "  Grok Bot  ")
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(issued.Value, ValuePrefix))
	assert.Len(t, issued.Value, len(ValuePrefix)+43, "32 random bytes in base64url")
	assert.Equal(t, "Grok Bot", issued.Token.Name)
	assert.Equal(t, issued.Value[len(ValuePrefix):len(ValuePrefix)+DisplayPrefixLen], issued.Token.Prefix)

	sum := sha256.Sum256([]byte(issued.Value))
	assert.Equal(t, hex.EncodeToString(sum[:]), column(`SELECT token_hash FROM mcp_token`))
	// No column of the row holds the value or the random part of it.
	row := column(`SELECT id || '|' || name || '|' || token_hash || '|' || token_prefix || '|' || created_at FROM mcp_token`)
	assert.NotContains(t, row, issued.Value[len(ValuePrefix):])

	other, err := s.Issue(ctx, "second")
	require.NoError(t, err)
	assert.NotEqual(t, issued.Value, other.Value)
}

func TestIssue_RejectsABadName(t *testing.T) {
	s, _, _ := newService(t)
	for _, name := range []string{"", "   ", strings.Repeat("あ", MaxNameRunes+1)} {
		_, err := s.Issue(context.Background(), name)
		var invalid *InvalidError
		require.ErrorAs(t, err, &invalid, "name %q", name)
		assert.Equal(t, "name", invalid.Field)
	}
	_, err := s.Issue(context.Background(), strings.Repeat("あ", MaxNameRunes))
	assert.NoError(t, err, "64 characters are allowed (counted in characters, not bytes)")
}

func TestVerify_AcceptsOnlyAnActiveToken(t *testing.T) {
	ctx := context.Background()
	s, _, _ := newService(t)
	issued, err := s.Issue(ctx, "a")
	require.NoError(t, err)

	got, err := s.Verify(ctx, issued.Value)
	require.NoError(t, err)
	assert.Equal(t, issued.Token.ID, got.ID)
	assert.True(t, s.Active(ctx, issued.Token.ID))

	for name, value := range map[string]string{
		"empty":         "",
		"no prefix":     issued.Value[len(ValuePrefix):],
		"unknown":       ValuePrefix + strings.Repeat("A", 43),
		"changed value": issued.Value[:len(issued.Value)-1] + "x",
	} {
		_, err := s.Verify(ctx, value)
		assert.ErrorIs(t, err, ErrInvalidToken, name)
	}

	require.NoError(t, s.Revoke(ctx, issued.Token.ID))
	_, err = s.Verify(ctx, issued.Value)
	assert.ErrorIs(t, err, ErrInvalidToken, "a revoked token")
	assert.False(t, s.Active(ctx, issued.Token.ID))
}

func TestRevoke_IsPerTokenAndKeepsTheFirstTime(t *testing.T) {
	ctx := context.Background()
	s, c, _ := newService(t)
	a, err := s.Issue(ctx, "a")
	require.NoError(t, err)
	b, err := s.Issue(ctx, "b")
	require.NoError(t, err)

	require.NoError(t, s.Revoke(ctx, a.Token.ID))
	first := c.t
	c.t = c.t.Add(time.Hour)
	require.NoError(t, s.Revoke(ctx, a.Token.ID), "revoking again is not an error")

	_, err = s.Verify(ctx, b.Value)
	assert.NoError(t, err, "the other token is untouched")
	list, err := s.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	for _, tok := range list {
		if tok.ID == a.Token.ID {
			require.NotNil(t, tok.RevokedAt)
			assert.True(t, tok.RevokedAt.Equal(first), "the first revocation time stays")
		} else {
			assert.Nil(t, tok.RevokedAt)
		}
	}
	assert.ErrorIs(t, s.Revoke(ctx, "missing"), ErrNotFound)
}

func TestVerify_RecordsLastUseAtMostOncePerInterval(t *testing.T) {
	ctx := context.Background()
	s, c, _ := newService(t)
	issued, err := s.Issue(ctx, "a")
	require.NoError(t, err)
	lastUsed := func() *time.Time {
		list, err := s.List(ctx)
		require.NoError(t, err)
		return list[0].LastUsedAt
	}
	assert.Nil(t, lastUsed(), "never used")

	_, err = s.Verify(ctx, issued.Value)
	require.NoError(t, err)
	first := c.t
	require.NotNil(t, lastUsed())
	assert.True(t, lastUsed().Equal(first))

	c.t = c.t.Add(touchInterval - time.Second)
	_, err = s.Verify(ctx, issued.Value)
	require.NoError(t, err)
	assert.True(t, lastUsed().Equal(first), "a second use within the interval writes nothing")

	c.t = c.t.Add(time.Second)
	_, err = s.Verify(ctx, issued.Value)
	require.NoError(t, err)
	assert.True(t, lastUsed().Equal(c.t), "the next use after the interval is recorded")
}
