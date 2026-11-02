package toolstatus_test

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/tokenstore"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/handler/toolstatus"
)

// text is what the settings source says, the lines joined (the source the settings overview reads for its API keys card).
func text(t *testing.T, svc toolstatus.Services) string {
	t.Helper()
	lines, err := svc.Settings(t.Context())
	require.NoError(t, err)
	var s string
	for _, l := range lines {
		s += l.Text + "\n"
	}
	return s
}

func TestSettingsSource_CountsTheKeysThatAreSet(t *testing.T) {
	keys := credentialtest.New()
	creds := service.NewCredentialService(keys)
	svc := toolstatus.Services{Credentials: creds}
	assert.Contains(t, text(t, svc), "API キー 0 / 14 件")
	assert.NotContains(t, text(t, svc), "ChatGPT", "no ChatGPT service: no second line")

	require.NoError(t, creds.Save("anthropic", "sk-test-fake-1"))
	require.NoError(t, creds.Save("openai", "sk-test-fake-2"))

	got := text(t, svc)
	assert.Contains(t, got, "API キー 2 / 14 件")
	assert.NotContains(t, got, "sk-test", "a key never reaches the page")
}

func newChatGPT(t *testing.T, keys *credentialtest.Memory, seed func(dataDir string, keys *credentialtest.Memory)) *chatgpt.Service {
	t.Helper()
	dir := t.TempDir()
	if seed != nil {
		seed(dir, keys)
	}
	svc := chatgpt.New(chatgpt.Options{Keys: keys, DataDir: dir, LockPath: filepath.Join(t.TempDir(), ".auth-refresh.lock"), Issuer: "http://127.0.0.1:9"})
	t.Cleanup(svc.Close)
	return svc
}

func signIn(kind string, reloginRequired bool) func(string, *credentialtest.Memory) {
	return func(_ string, keys *credentialtest.Memory) {
		err := tokenstore.New(keys, kind).Save(tokenstore.Tokens{
			Access: "fake-access", Refresh: "fake-refresh", IDToken: "fake-id",
			Meta: tokenstore.Meta{ClientID: "oaiapp_x", ReloginRequired: reloginRequired},
		})
		if err != nil {
			panic(err)
		}
	}
}

func TestSettingsSource_ChatGPTLogin(t *testing.T) {
	codexSignedIn := func(dir string, _ *credentialtest.Memory) {
		if err := os.WriteFile(filepath.Join(dir, codex.StateFile), []byte(`{"account":{"signed_in":true}}`), 0o600); err != nil {
			panic(err)
		}
	}
	tests := []struct {
		name string
		seed func(string, *credentialtest.Memory)
		want string
	}{
		{"nobody signed in", nil, "ChatGPT: 未ログイン"},
		{"official", signIn("official", false), "ChatGPT: ログイン済み"},
		{"unofficial image generation", signIn("image", false), "ChatGPT: ログイン済み"},
		{"codex", codexSignedIn, "ChatGPT: ログイン済み"},
		{"official has to sign in again", signIn("official", true), "ChatGPT: 再ログインが必要"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys := credentialtest.New()
			svc := toolstatus.Services{Credentials: service.NewCredentialService(keys), ChatGPT: newChatGPT(t, keys, tt.seed)}

			got := text(t, svc)

			assert.Contains(t, got, tt.want)
			assert.NotContains(t, got, "fake-", "no token reaches the page")
		})
	}
}

// hangingKeys is a key store whose first Has waits for release; it counts the calls.
type hangingKeys struct {
	credential.Store
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (k *hangingKeys) Has(provider string) (bool, error) {
	if k.calls.Add(1) == 1 {
		close(k.entered)
		<-k.release
	}
	return k.Store.Has(provider)
}

// The settings read of a request that gave up stops after the key store call it is in, instead of asking the OS key
// store about the other providers: abandoned reads do not pile up on a hanging store.
func TestSettingsSource_AnAbandonedReadStopsAskingTheKeyStore(t *testing.T) {
	keys := &hangingKeys{Store: credentialtest.New(), entered: make(chan struct{}), release: make(chan struct{})}
	src := toolstatus.Services{Credentials: service.NewCredentialService(keys)}.Settings
	ctx, cancel := context.WithCancel(t.Context())
	errc := make(chan error, 1)
	go func() {
		_, err := src(ctx)
		errc <- err
	}()
	<-keys.entered

	cancel() // the request gave up
	close(keys.release)

	require.ErrorIs(t, <-errc, context.Canceled)
	assert.EqualValues(t, 1, keys.calls.Load(), "no further provider is asked after the request is gone")
}
