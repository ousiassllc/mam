package providerbackends_test

import (
	"context"
	"encoding/base64"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/viaprovider"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

const videoBytes = "FAKE-MP4"

// seen is one request the fake aggregator got (the fakes are httptest servers: nothing here is a real API).
type seen struct {
	method, path, auth string
	body               map[string]any
}

// backends builds the backends of the whole setting with every vendor at the fake server and the provider selected.
func backends(t *testing.T, srv *httptest.Server, provider viaprovider.Provider, keys map[string]string) (modeladapter.Config, modeladapter.Backends) {
	t.Helper()
	cfg := modeladapter.DefaultConfig()
	cfg.VideoProvider = provider
	for v, vc := range cfg.Vendors {
		vc.BaseURL = srv.URL
		cfg.Vendors[v] = vc
	}
	store := credentialtest.New()
	for p, k := range keys {
		require.NoError(t, store.Set(p, k))
	}
	opts := localbackends.Options{PollEvery: time.Millisecond, StagingDir: t.TempDir(), Vendor: []modeladapter.BackendOption{modeladapter.WithLocalDownloads()}}
	return cfg, localbackends.Build(cfg, store, opts)
}

func frame(s string) *modeladapter.Input {
	return &modeladapter.Input{Data: []byte(s), MimeType: "image/png"}
}

func dataURI(s string) string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte(s))
}

func generate(t *testing.T, b modeladapter.Backends, m domain.ModelName, req modeladapter.VideoRequest) (modeladapter.VideoResult, error) {
	t.Helper()
	return b.Video[m].GenerateVideo(context.Background(), "ignored", req)
}

func readVideo(t *testing.T, res modeladapter.VideoResult) {
	t.Helper()
	got, err := res.File.Bytes()
	require.NoError(t, err)
	assert.Equal(t, videoBytes, string(got))
	assert.Equal(t, "video/mp4", res.MimeType)
	res.File.Discard()
}
