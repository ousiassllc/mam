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

func TestRuntimeLayoutIsOneDirectoryPerRuntimeBelowLocalaiNextToTheExecutable(t *testing.T) {
	root := store.LocalAIRoot(filepath.Join("install", "MAM", "mam.exe"))
	assert.Equal(t, filepath.Join("install", "MAM", "localai"), root)
	l := store.ForRuntime(root, "gpt-sovits")
	assert.Equal(t, filepath.Join(root, "gpt-sovits"), l.Root)
	for _, p := range []string{l.EnvDir(), l.EnvNewDir(), l.EnvOldDir(), l.DownloadsDir(), l.CacheDir(), l.TempDir(), l.LogPath(), l.StatePath()} {
		rel, err := filepath.Rel(l.Root, p)
		require.NoError(t, err)
		assert.NotContains(t, rel, "..", p+" is below the runtime's root: removing the root removes everything")
	}
	assert.Equal(t, filepath.Join(l.Root, "env"), l.EnvDir())
	assert.Equal(t, filepath.Join(l.Root, "downloads"), l.DownloadsDir())
	assert.Equal(t, filepath.Join(l.Root, "logs", "runtime.log"), l.LogPath())
	assert.Equal(t, filepath.Join(l.Root, "state.json"), l.StatePath())
	assert.NotEqual(t, store.ForRuntime(root, "musetalk").Root, l.Root, "every runtime has its own directory")
}

func TestRuntimeLayoutCheckWritable(t *testing.T) {
	l := store.ForRuntime(filepath.Join(t.TempDir(), "localai"), "gpt-sovits")
	require.NoError(t, l.CheckWritable())
	entries, _ := os.ReadDir(l.Root)
	assert.Empty(t, entries, "the probe file is removed")

	blocked := filepath.Join(t.TempDir(), "localai")
	require.NoError(t, os.WriteFile(blocked, []byte("x"), 0o600))
	assert.ErrorIs(t, store.ForRuntime(blocked, "gpt-sovits").CheckWritable(), store.ErrNotWritable)
}

func TestRuntimeStatePersistsAndIsCopiedOnRead(t *testing.T) {
	l := store.ForRuntime(t.TempDir(), "gpt-sovits")
	s, err := store.OpenRuntime(l)
	require.NoError(t, err)
	assert.Equal(t, store.RuntimeState{}, s.Get(), "no file: the empty state")
	assert.NoFileExists(t, l.StatePath(), "opening writes nothing")

	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, s.Update(func(r *store.RuntimeState) {
		r.AutoStart, r.LastPort = true, 9880
		r.Installed = &store.RuntimeRecord{Version: "20250604", Variant: "nvidia", InstalledAt: at}
	}))
	got := s.Get()
	got.Installed.Version = "changed by the caller"
	assert.Equal(t, "20250604", s.Get().Installed.Version, "a caller cannot change what the store holds")

	again, err := store.OpenRuntime(l)
	require.NoError(t, err)
	assert.Equal(t, s.Get(), again.Get())
	assert.True(t, again.Get().AutoStart)
	assert.Equal(t, 9880, again.Get().LastPort)
}

func TestRuntimeStateThatCannotBeReadIsMovedAsideAndMamStarts(t *testing.T) {
	l := store.ForRuntime(t.TempDir(), "gpt-sovits")
	require.NoError(t, os.MkdirAll(l.Root, 0o750))
	require.NoError(t, os.WriteFile(l.StatePath(), []byte("{broken"), 0o600))
	s, err := store.OpenRuntime(l)
	require.NoError(t, err)
	assert.Equal(t, store.RuntimeState{}, s.Get())
	assert.FileExists(t, l.StatePath()+store.CorruptSuffix)
	assert.NoFileExists(t, l.StatePath())
}

func TestRuntimeStateKeepsItselfWhenTheWriteFails(t *testing.T) {
	root := t.TempDir()
	l := store.ForRuntime(root, "gpt-sovits")
	s, err := store.OpenRuntime(l)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "gpt-sovits"), []byte("x"), 0o600)) // a file where the directory must be made
	require.Error(t, s.Update(func(r *store.RuntimeState) { r.AutoStart = true }))
	assert.False(t, s.Get().AutoStart, "nothing changes when the write fails")
}
