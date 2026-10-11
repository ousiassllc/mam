package jev_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/jev"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

type memKeys map[string]string

func (m memKeys) Set(p, k string) error { m[p] = k; return nil }
func (m memKeys) Get(p string) (string, error) {
	if k, ok := m[p]; ok {
		return k, nil
	}
	return "", credential.ErrNotFound
}
func (m memKeys) Delete(p string) error      { delete(m, p); return nil }
func (m memKeys) Has(p string) (bool, error) { _, ok := m[p]; return ok, nil }

func newClient(t *testing.T, srv *httptest.Server, keys memKeys) *jev.Client {
	t.Helper()
	cfg := jev.DefaultConfig()
	cfg.BaseURL = srv.URL
	c, err := jev.New(cfg, keys, srv.Client())
	require.NoError(t, err)
	return c
}

func TestChoose_SendsOnlyStructuredIssueFieldsAtAPinnedVersionWithTheStoredKey(t *testing.T) {
	var gotAuth, gotPath string
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(raw, &got))
		_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{"repair_strategy":{"type":"choice","choice":"prompt_repair","probabilities":{"prompt_repair":0.8,"full_retake":0.2},"confidence":0.8}},"usage":{"input_tokens":1000,"output_tokens":5}}`))
	}))
	defer srv.Close()

	c := newClient(t, srv, memKeys{jev.CredentialProvider: "secret-key"})
	out, err := c.Choose(context.Background(), jev.Input{
		Issues: []domain.JudgeIssue{{
			Type: "camera_speed", Severity: domain.SeverityMedium, Dimension: "camera_motion", TimeRange: "0.8-1.7s",
			Description: "カメラが速すぎる", CharacterID: "Mira", Frames: []int{20, 43}, LockViolation: true,
		}},
		Candidates: []domain.RepairAction{domain.RepairPrompt, domain.RepairFullRetake},
	})
	require.NoError(t, err)

	assert.Equal(t, "/v1/systemone", gotPath)
	assert.Equal(t, "Bearer secret-key", gotAuth)
	assert.Equal(t, "jev-1.13.0", got["model"], "a pinned version, never jev-latest")
	state, _ := json.Marshal(got["state"])
	assert.JSONEq(t, `{"issues":[{"type":"camera_speed","severity":"medium","dimension":"camera_motion","time_range":"0.8-1.7s"}]}`, string(state),
		"no free text (Japanese description), no character or frame data")
	q := got["questions"].(map[string]any)["repair_strategy"].(map[string]any)
	assert.Equal(t, "choice", q["type"])
	assert.ElementsMatch(t, []string{"prompt_repair", "full_retake"}, keys(q["criteria"].(map[string]any)), "only the candidates are offered")

	assert.Equal(t, domain.RepairPrompt, out.Action)
	assert.InDelta(t, 0.8, out.Confidence, 1e-9)
	assert.InDelta(t, 0.8, out.Probabilities["prompt_repair"], 1e-9)
	assert.Equal(t, 1000, out.InputTokens)
	assert.InDelta(t, 1000*jev.DefaultInputCostPerMillionUSD/1e6, out.CostUSD, 1e-12)
	assert.Positive(t, out.Latency)
}

func TestChoose_WithoutAKeyItDoesNotCallTheAPI(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer srv.Close()

	_, err := newClient(t, srv, memKeys{}).Choose(context.Background(), jev.Input{Candidates: []domain.RepairAction{domain.RepairFullRetake}})
	assert.ErrorIs(t, err, jev.ErrNoAPIKey)
	assert.False(t, called)
}

func TestChoose_FailuresAreErrors(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"status": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) },
		"junk":   func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`nope`)) },
		"empty":  func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"answers":{}}`)) },
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(handler)
			defer srv.Close()
			_, err := newClient(t, srv, memKeys{jev.CredentialProvider: "k"}).Choose(context.Background(), jev.Input{Candidates: []domain.RepairAction{domain.RepairFullRetake}})
			assert.Error(t, err)
		})
	}
}

func TestConfig_TheVersionMustBePinned(t *testing.T) {
	for _, model := range []string{"jev-latest", "jev-preview", "jev-1.13", ""} {
		cfg := jev.DefaultConfig()
		cfg.Model = model
		assert.Error(t, cfg.Validate(), model)
		_, err := jev.New(cfg, memKeys{}, nil)
		assert.Error(t, err, model)
	}
	assert.NoError(t, jev.DefaultConfig().Validate())
}

func TestLoadConfig_OverridesTheDefaultsAndRejectsUnpinnedAndUnknown(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) string {
		p := filepath.Join(dir, "jev.json")
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
		return p
	}

	cfg, err := jev.LoadConfig("")
	require.NoError(t, err)
	assert.Equal(t, jev.DefaultConfig(), cfg)

	cfg, err = jev.LoadConfig(write(`{"model":"jev-2.0.1","timeout_seconds":5}`))
	require.NoError(t, err)
	assert.Equal(t, "jev-2.0.1", cfg.Model)
	assert.Equal(t, 5, cfg.TimeoutSeconds)
	assert.Equal(t, jev.DefaultBaseURL, cfg.BaseURL, "unset keys keep the default")

	_, err = jev.LoadConfig(write(`{"model":"jev-latest"}`))
	assert.Error(t, err)
	_, err = jev.LoadConfig(write(`{"mdl":"jev-2.0.1"}`))
	assert.Error(t, err, "unknown keys are an error")
	_, err = jev.LoadConfig(filepath.Join(dir, "missing.json"))
	assert.Error(t, err)
}

func keys(m map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestDefaultClientDoesNotForwardTheKeyOnARedirectToAnotherHost(t *testing.T) {
	var foreignHits atomic.Int32
	foreign := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { foreignHits.Add(1) }))
	defer foreign.Close()
	vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, foreign.URL+"/steal", http.StatusFound)
	}))
	defer vendor.Close()
	cfg := jev.DefaultConfig()
	cfg.BaseURL = vendor.URL
	c, err := jev.New(cfg, memKeys{jev.CredentialProvider: "secret-key"}, nil)
	require.NoError(t, err)

	_, err = c.Choose(context.Background(), jev.Input{
		Issues:     []domain.JudgeIssue{{Type: "camera_speed", Severity: domain.SeverityMedium}},
		Candidates: []domain.RepairAction{domain.RepairPrompt, domain.RepairFullRetake},
	})

	require.Error(t, err)
	assert.Zero(t, foreignHits.Load())
}
