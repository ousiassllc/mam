package logging_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/logging"
)

// Obviously fake values in the shapes of the real ones.
const (
	fakeMCP    = "mam_mcp_FAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKE000"
	fakeHF     = "hf_FAKEFAKEFAKEFAKEFAKE0000"
	fakeSK     = "sk-ant-FAKEFAKEFAKEFAKEFAKE0000"
	fakeGoogle = "AIzaFAKEFAKEFAKEFAKEFAKEFAKE0000"
	fakeJWT    = "eyJFAKEFAKE0.eyJFAKEFAKE1.FAKESIGFAKE"
)

func TestRedact_HidesTheShapesOfTheSecretsMamHandles(t *testing.T) {
	for name, in := range map[string]string{
		"mcp token":   "issued " + fakeMCP + " ok",
		"hf token":    "download with " + fakeHF,
		"vendor key":  "key " + fakeSK,
		"google key":  "key " + fakeGoogle,
		"jwt":         "id " + fakeJWT,
		"bearer":      "Authorization: Bearer abcdefghijklmnop0000",
		"query key":   "GET https://api.example.test/v1/models?key=FAKEVALUE0000&alt=json",
		"json field":  `{"api_key":"FAKEVALUE0000","x":1}`,
		"assignment":  "token=FAKEVALUE0000 retry",
		"header":      "x-goog-api-key: FAKEVALUE0000",
		"upper":       "PASSWORD = FAKEVALUE0000",
		"secret text": "client_secret:FAKEVALUE0000",
	} {
		out := logging.Redact(in)
		assert.NotContains(t, out, "FAKE", name)
		assert.Contains(t, out, "[redacted]", name)
	}
	assert.Equal(t, "no secret here: shot s-1 took 3s", logging.Redact("no secret here: shot s-1 took 3s"))
	assert.Equal(t, "tokens used: 12", logging.Redact("tokens used: 12"), "a word is not a field")
}

func TestReader_NeverReturnsOrConfirmsASecretInTheLog(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "mam.log",
		line(0, "ERROR", "call failed: "+fakeSK, "err", `"Get \"https://x.test/?key=FAKEVALUE0000\": timeout"`, "api_key", `"FAKEVALUE0000"`, "token", `"`+fakeMCP+`"`, "total_tokens", "812")+
			line(time.Second, "INFO", "plain"))

	res := read(t, dir, logging.Query{})
	require.Len(t, res.Entries, 2)
	got := res.Entries[1]
	assert.NotContains(t, got.Message, "FAKE")
	assert.Equal(t, "[redacted]", got.Attrs["api_key"], "a string under a secret-like key goes whole")
	assert.Equal(t, "[redacted]", got.Attrs["token"])
	assert.NotContains(t, got.Attrs["err"], "FAKEVALUE")
	assert.EqualValues(t, "812", strings.TrimSpace(strings.Trim(fmtAny(got.Attrs["total_tokens"]), `"`)), "a count named like a token is not a secret")

	// A filter cannot probe for the secret: it is matched after it was hidden.
	assert.Empty(t, read(t, dir, logging.Query{Keyword: "FAKEVALUE0000"}).Entries)
	assert.Empty(t, read(t, dir, logging.Query{Keyword: fakeSK}).Entries)
	assert.Empty(t, read(t, dir, logging.Query{Attrs: map[string]string{"api_key": "FAKEVALUE0000"}}).Entries)
	assert.Len(t, read(t, dir, logging.Query{Keyword: "[redacted]"}).Entries, 1)
}

func fmtAny(v any) string { return fmt.Sprint(v) }

func TestRedact_HidesTheLiteralSecretsTheCallerKnowsInAnyShape(t *testing.T) {
	assert.Equal(t, "call failed with [redacted] twice [redacted]", logging.Redact("call failed with plain-secret-value twice plain-secret-value", "plain-secret-value"))
	assert.Equal(t, "short stays: abc", logging.Redact("short stays: abc", "abc"), "a value shorter than 8 characters would hide ordinary words")
	assert.Equal(t, "unchanged", logging.Redact("unchanged", "", "other-secret-value"))

	dir := t.TempDir()
	write(t, dir, "mam.log", line(0, "ERROR", "failed: plain-secret-value", "err", `"dial plain-secret-value"`, "list", `["plain-secret-value"]`))
	res, err := logging.Reader{Dir: dir}.Read(t.Context(), logging.Query{Limit: 5, Secrets: []string{"plain-secret-value"}})
	require.NoError(t, err)
	require.Len(t, res.Entries, 1)
	assert.Equal(t, "failed: [redacted]", res.Entries[0].Message)
	assert.Equal(t, "dial [redacted]", res.Entries[0].Attrs["err"])
	assert.NotContains(t, fmt.Sprint(res.Entries[0].Attrs["list"]), "plain-secret-value")
}
