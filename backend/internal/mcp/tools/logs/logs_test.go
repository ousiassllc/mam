package logs_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/credentials"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/logs"
)

func register(env *mcptest.Env, r *mcpkit.Registrar) {
	logs.Register(r, env.Services)
	credentials.Register(r, env.Services)
	// A tool whose failure carries a secret in its cause, as an adapter's error with a URL or a header could.
	mcpkit.Read(r, "fail_with_secret", "テスト用: 原因に秘密を含む internal エラーで失敗する / Fails with a secret in the cause.",
		func(context.Context, struct{}) (struct{}, error) {
			return struct{}{}, errors.New(`Get "https://api.example.test/v1/models?key=` + fakeKey + `": 401 api_key=` + fakeKey)
		})
}

func newEnv(t *testing.T) *mcptest.Env { return mcptest.New(t, register) }

// Obviously fake credentials in the shape of real ones.
const (
	fakeKey   = "FAKEKEYFAKEKEY0000"
	fakeToken = "mam_mcp_FAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKE0000"
)

func get(t *testing.T, env *mcptest.Env, args map[string]any) logs.Output {
	t.Helper()
	var out logs.Output
	env.CallOK("get_app_logs", args, &out)
	return out
}

func msgs(out logs.Output) []string {
	var m []string
	for _, e := range out.Entries {
		m = append(m, e.Message)
	}
	return m
}

// day is a UTC day a few days back: lumberjack deletes generations older than 30 days when the logger of the
// test environment starts, so a generation the test writes by hand must be recent.
var day = time.Now().UTC().Truncate(24 * time.Hour).Add(-5 * 24 * time.Hour).Add(9 * time.Hour)

func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15-04-05.000") }

func rfc(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func writeGeneration(t *testing.T, env *mcptest.Env, name string, lines ...string) {
	t.Helper()
	data := []byte(strings.Join(lines, "\n") + "\n")
	if strings.HasSuffix(name, ".gz") {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		_, err := zw.Write(data)
		require.NoError(t, err)
		require.NoError(t, zw.Close())
		data = buf.Bytes()
	}
	require.NoError(t, os.WriteFile(filepath.Join(env.LogDir, name), data, 0o600))
}

func jsonLine(at time.Time, level, msg string, attrs map[string]any) string {
	m := map[string]any{"time": at.Format(time.RFC3339Nano), "level": level, "msg": msg}
	for k, v := range attrs {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func TestGetAppLogs_IsAReadOnlyToolInTheList(t *testing.T) {
	env := newEnv(t)
	list, err := env.Client.ListTools(context.Background(), nil)
	require.NoError(t, err)
	var tool *mcp.Tool
	for _, tl := range list.Tools {
		if tl.Name == "get_app_logs" {
			tool = tl
		}
	}
	require.NotNil(t, tool)
	assert.True(t, tool.Annotations.ReadOnlyHint)
	assert.NotEmpty(t, tool.Description)
}

func TestGetAppLogs_FiltersByLevelKeywordAndIDs(t *testing.T) {
	env := newEnv(t)
	env.Logger.Info("shot generation started", "project_id", "p-1", "shot_id", "s-1", "job_id", "job_A")
	env.Logger.Warn("rate limited", "project_id", "p-1", "shot_id", "s-2")
	env.Logger.Error("job failed", "project_id", "p-2", "job_id", "job_B", "error_id", "err_B", "err", "upstream refused")
	env.Logger.Debug("noise", "project_id", "p-1")

	all := get(t, env, map[string]any{})
	assert.Equal(t, []string{"job failed", "rate limited", "shot generation started"}, filter(msgs(all), "mcp "), "newest first, info and above by default")
	assert.False(t, all.Truncated)

	assert.Equal(t, []string{"job failed"}, msgs(get(t, env, map[string]any{"level": "error"})))
	assert.Equal(t, []string{"job failed", "rate limited"}, msgs(get(t, env, map[string]any{"level": "warn"})))
	assert.Contains(t, msgs(get(t, env, map[string]any{"level": "debug"})), "noise")
	assert.Equal(t, []string{"shot generation started"}, msgs(get(t, env, map[string]any{"keyword": "GENERATION"})))
	assert.Equal(t, []string{"job failed"}, msgs(get(t, env, map[string]any{"keyword": "refused"})), "an attribute value matches a keyword")

	assert.Equal(t, []string{"rate limited", "shot generation started"}, msgs(get(t, env, map[string]any{"attrs": map[string]any{"project_id": "p-1"}})))
	assert.Equal(t, []string{"shot generation started"}, msgs(get(t, env, map[string]any{"attrs": map[string]any{"project_id": "p-1", "shot_id": "s-1"}})))
	assert.Equal(t, []string{"job failed"}, msgs(get(t, env, map[string]any{"attrs": map[string]any{"job_id": "job_B"}})))
	byErr := get(t, env, map[string]any{"attrs": map[string]any{"error_id": "err_B"}})
	require.Len(t, byErr.Entries, 1)
	assert.Equal(t, "ERROR", byErr.Entries[0].Level)
	assert.Equal(t, "upstream refused", byErr.Entries[0].Attrs["err"])
	assert.Empty(t, get(t, env, map[string]any{"attrs": map[string]any{"job_id": "nope"}}).Entries)
	assert.NotNil(t, get(t, env, map[string]any{"keyword": "no such text"}).Entries, "an empty answer is [] not null")
}

// filter drops the messages that start with a prefix (the log of the test environment's own services).
func filter(in []string, prefix string) []string {
	var out []string
	for _, m := range in {
		if !strings.HasPrefix(m, prefix) {
			out = append(out, m)
		}
	}
	return out
}

func TestGetAppLogs_ReadsRotatedGenerationsAndFiltersByTime(t *testing.T) {
	env := newEnv(t)
	old := day
	writeGeneration(t, env, "mam-"+stamp(old.Add(30*time.Minute))+".log.gz",
		jsonLine(old, "INFO", "gz one", nil), jsonLine(old.Add(time.Minute), "ERROR", "gz two", map[string]any{"job_id": "job_old"}))
	writeGeneration(t, env, "mam-"+stamp(old.Add(24*time.Hour+30*time.Minute))+".log",
		jsonLine(old.Add(24*time.Hour), "INFO", "plain one", nil), "garbage that is not a log line")
	env.Logger.Info("active one")

	assert.Equal(t, []string{"active one", "plain one", "gz two", "gz one"}, msgs(get(t, env, map[string]any{})))
	assert.Equal(t, []string{"gz two"}, msgs(get(t, env, map[string]any{"attrs": map[string]any{"job_id": "job_old"}})), "an id in a compressed generation")

	assert.Equal(t, []string{"gz two", "gz one"}, msgs(get(t, env, map[string]any{"until": rfc(old.Add(3 * time.Hour))})))
	assert.Equal(t, []string{"gz two"}, msgs(get(t, env, map[string]any{"since": rfc(old.Add(30 * time.Second)), "until": rfc(old.Add(3 * time.Hour))})))
	assert.Equal(t, []string{"active one", "plain one"}, msgs(get(t, env, map[string]any{"since": rfc(old.Add(12 * time.Hour))})))

	// Offsets are honoured: the same instant in +09:00.
	jst := time.FixedZone("JST", 9*3600)
	assert.Equal(t, []string{"gz two"}, msgs(get(t, env, map[string]any{
		"since": old.Add(30 * time.Second).In(jst).Format(time.RFC3339), "until": old.Add(3 * time.Hour).In(jst).Format(time.RFC3339)})))
}

func TestGetAppLogs_LimitAndNextUntilPageThroughTheLog(t *testing.T) {
	env := newEnv(t)
	base := day
	var lines []string
	for i := range 7 {
		lines = append(lines, jsonLine(base.Add(time.Duration(i)*time.Second), "INFO", fmt.Sprintf("line %d", i), nil))
	}
	writeGeneration(t, env, "mam-"+stamp(day.Add(time.Hour))+".log", lines...)

	var seen []string
	args := map[string]any{"limit": 3, "until": rfc(day.Add(30 * time.Minute))}
	for page := 0; ; page++ {
		require.Less(t, page, 5, "paging must end")
		out := get(t, env, args)
		seen = append(seen, msgs(out)...)
		if !out.Truncated {
			assert.Empty(t, out.NextUntil)
			break
		}
		require.NotEmpty(t, out.NextUntil)
		args["until"] = out.NextUntil
	}
	assert.Equal(t, []string{"line 6", "line 5", "line 4", "line 3", "line 2", "line 1", "line 0"}, seen)
}

func TestGetAppLogs_RefusesBadFilters(t *testing.T) {
	env := newEnv(t)
	for name, args := range map[string]map[string]any{
		"level":      {"level": "loud"},
		"since":      {"since": "yesterday"},
		"until":      {"until": "2026-13-01"},
		"order":      {"since": "2026-09-02T00:00:00Z", "until": "2026-09-01T00:00:00Z"},
		"limit low":  {"limit": -1},
		"limit high": {"limit": logs.MaxLimit + 1},
		"attr key":   {"attrs": map[string]any{" ": "x"}},
	} {
		assert.Contains(t, env.CallError("get_app_logs", args), "validation", name)
	}
	tooMany := map[string]any{}
	for i := range 11 {
		tooMany[fmt.Sprintf("k%d", i)] = "v"
	}
	assert.Contains(t, env.CallError("get_app_logs", map[string]any{"attrs": tooMany}), "validation")
}

func TestGetAppLogs_TheLogSurvivesARestart(t *testing.T) {
	env := newEnv(t)
	env.Logger.Info("written before the restart", "project_id", "p-r")
	after := env.Restart(register)
	assert.Equal(t, []string{"written before the restart"}, msgs(get(t, after, map[string]any{"attrs": map[string]any{"project_id": "p-r"}})))
}

func TestGetAppLogs_WithoutALogDirectoryIsUnavailable(t *testing.T) {
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		r.LogDir = ""
		logs.Register(r, env.Services)
	})
	assert.Contains(t, env.CallError("get_app_logs", map[string]any{}), "unavailable")
}

func TestGetAppLogs_TheOutputIsBounded(t *testing.T) {
	env := newEnv(t)
	base := day
	var lines []string
	for i := range 300 {
		lines = append(lines, jsonLine(base.Add(time.Duration(i)*time.Second), "INFO", "big", map[string]any{"pad": strings.Repeat("x", 1900)}))
	}
	writeGeneration(t, env, "mam-"+stamp(day.Add(time.Hour))+".log", lines...)
	res := env.Call("get_app_logs", map[string]any{"until": rfc(day.Add(30 * time.Minute)), "limit": logs.MaxLimit})
	require.False(t, res.IsError)
	raw, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)
	assert.Less(t, len(raw), 260<<10, "the answer stays near 200 KB however many lines match")
	var out logs.Output
	require.NoError(t, json.Unmarshal(raw, &out))
	assert.True(t, out.Truncated)
	assert.NotEmpty(t, out.NextUntil)
}

// Nothing a client can read of the tool, of the log it reads or of the calls that handle a key holds the key.
func TestGetAppLogs_NeverReturnsASecret(t *testing.T) {
	env := newEnv(t)
	const apiKey = "sk-kling-FAKEKEYFAKEKEY-12345678"

	// The real credential tools handle a key: neither a success nor a refusal puts it in the log.
	env.CallOK("set_credential", map[string]any{"provider": "kling", "api_key": apiKey}, nil)
	env.CallError("set_credential", map[string]any{"provider": "kling", "api_key": "has space " + apiKey})
	env.CallError("set_credential", map[string]any{"provider": "no-such-vendor", "api_key": apiKey})
	env.CallOK("delete_credential", map[string]any{"provider": "kling"}, nil)
	raw, err := os.ReadFile(filepath.Join(env.LogDir, "mam.log"))
	if err == nil {
		assert.NotContains(t, string(raw), apiKey, "the credential tools write the key nowhere in the log")
		assert.NotContains(t, string(raw), "FAKEKEYFAKEKEY")
	}

	// A cause that does carry a secret (an adapter error with a URL) is in the log as the tool logs it ...
	assert.Contains(t, env.CallError("fail_with_secret", map[string]any{}), "internal")
	raw, err = os.ReadFile(filepath.Join(env.LogDir, "mam.log"))
	require.NoError(t, err)
	require.Contains(t, string(raw), fakeKey, "the log itself has the secret: the test needs the reader's defence to matter")

	// ... and the tool does not return it, however the call is shaped, nor lets a filter find it.
	for name, args := range map[string]map[string]any{
		"all":       {"level": "debug"},
		"error":     {"level": "error"},
		"tool":      {"attrs": map[string]any{"tool": "fail_with_secret"}},
		"keyword":   {"keyword": fakeKey},
		"attribute": {"attrs": map[string]any{"err": fakeKey}},
		"mcp token": {"keyword": fakeToken},
	} {
		res := env.Call("get_app_logs", args)
		require.False(t, res.IsError, name)
		raw, err := json.Marshal(res)
		require.NoError(t, err)
		assert.NotContains(t, string(raw), fakeKey, name)
		assert.NotContains(t, string(raw), apiKey, name)
	}
	failed := get(t, env, map[string]any{"attrs": map[string]any{"tool": "fail_with_secret"}})
	require.Len(t, failed.Entries, 1, "the failure itself is readable")
	assert.Equal(t, "mcp tool failed", failed.Entries[0].Message)
	assert.Contains(t, fmt.Sprint(failed.Entries[0].Attrs["err"]), "[redacted]")
	assert.Empty(t, get(t, env, map[string]any{"keyword": fakeKey}).Entries, "a filter cannot confirm that a secret is in the log")

	// A token value written to the log by mistake is hidden too.
	env.Logger.Error("auth failed for "+fakeToken, "authorization", "Bearer "+fakeToken, "mcp_token", fakeToken)
	leak := get(t, env, map[string]any{"level": "error"})
	rawOut, err := json.Marshal(leak)
	require.NoError(t, err)
	assert.NotContains(t, string(rawOut), "FAKEFAKEFAKE")
}

// A key stored in the key store is hidden wherever it shows in the log, whatever its shape.
func TestGetAppLogs_HidesAStoredKeyInAnyShape(t *testing.T) {
	env := newEnv(t)
	const odd = "plain-words-key-0001" // no prefix, no pattern: only the key store knows it
	env.CallOK("set_credential", map[string]any{"provider": "kling", "api_key": odd}, nil)
	env.Logger.Error("kling answered 401 for "+odd, "err", "bad key "+odd, "detail", map[string]any{"key": odd})

	out := get(t, env, map[string]any{"level": "error"})
	require.Len(t, out.Entries, 1)
	raw, err := json.Marshal(out)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), odd)
	assert.Contains(t, out.Entries[0].Message, "[redacted]")
	assert.Empty(t, get(t, env, map[string]any{"keyword": odd}).Entries, "no probing for the key")
}
