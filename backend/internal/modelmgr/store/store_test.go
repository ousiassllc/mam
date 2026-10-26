package store_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

func TestLayoutIsOneDirectoryNextToTheExecutable(t *testing.T) {
	l := store.ForExecutable(filepath.Join("install", "MAM", "mam.exe"))
	assert.Equal(t, filepath.Join("install", "MAM", "comfyui"), l.Root)
	for _, p := range []string{l.EngineDir(), l.EngineNewDir(), l.EngineOldDir(), l.DataDir(), l.DownloadsDir(), l.LogPath(), l.StatePath(), l.ModelPath("models/vae/x.safetensors")} {
		rel, err := filepath.Rel(l.Root, p)
		require.NoError(t, err)
		assert.NotContains(t, rel, "..", p+" is below the root: removing the root removes everything")
	}
	assert.Equal(t, filepath.Join(l.Root, "data", "models", "vae", "x.safetensors"), l.ModelPath("models/vae/x.safetensors"))
}

func TestCheckWritable(t *testing.T) {
	l := store.Layout{Root: filepath.Join(t.TempDir(), "comfyui")}
	require.NoError(t, l.CheckWritable())
	entries, _ := os.ReadDir(l.Root)
	assert.Empty(t, entries, "the probe file is removed")

	// A file where the directory should be: not writable.
	blocked := filepath.Join(t.TempDir(), "comfyui")
	require.NoError(t, os.WriteFile(blocked, []byte("x"), 0o600))
	assert.ErrorIs(t, store.Layout{Root: blocked}.CheckWritable(), store.ErrNotWritable)
}

func TestStatePersistsAndIsCopiedOnRead(t *testing.T) {
	l := store.Layout{Root: t.TempDir()}
	s, err := store.Open(l)
	require.NoError(t, err)
	assert.Empty(t, s.Get().Models)

	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, s.Update(func(st *store.State) {
		st.AutoStart = true
		st.Engine = &store.EngineRecord{Version: "v0.42.1", Variant: "nvidia", InstalledAt: now}
		st.Consents = map[string]store.Consent{"x": {Version: "1", AcceptedAt: now}}
		st.Models = map[string]store.InstalledModel{"m": {Files: []store.InstalledFile{{Path: "models/vae/a", Size: 1}}}}
	}))
	got := s.Get()
	got.Models["other"] = store.InstalledModel{}
	got.Engine.Version = "changed"
	assert.Len(t, s.Get().Models, 1, "a copy")
	assert.Equal(t, "v0.42.1", s.Get().Engine.Version)

	again, err := store.Open(l)
	require.NoError(t, err)
	assert.Equal(t, s.Get(), again.Get())
	assert.True(t, again.Get().AutoStart)
}

func TestAFailedWriteChangesNothing(t *testing.T) {
	l := store.Layout{Root: filepath.Join(t.TempDir(), "missing", "deeper")}
	s, err := store.Open(l)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(l.Root), 0o750))
	require.NoError(t, os.WriteFile(l.Root, []byte("a file in the way"), 0o600))
	assert.Error(t, s.Update(func(st *store.State) { st.AutoStart = true }))
	assert.False(t, s.Get().AutoStart)
}

func TestAnUnreadableRecordIsMovedAsideAndTheStateStartsEmpty(t *testing.T) {
	l := store.Layout{Root: t.TempDir()}
	require.NoError(t, os.WriteFile(l.StatePath(), []byte("{broken"), 0o600))
	s, err := store.Open(l)
	require.NoError(t, err, "a damaged file must not keep mam from starting")
	assert.Empty(t, s.Get().Models)
	assert.NoFileExists(t, l.StatePath())
	kept, err := os.ReadFile(l.StatePath() + store.CorruptSuffix)
	require.NoError(t, err)
	assert.Equal(t, "{broken", string(kept))
	require.NoError(t, s.Update(func(st *store.State) { st.AutoStart = true }), "and it can be written again")
}
