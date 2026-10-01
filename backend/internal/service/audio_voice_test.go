package service_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// cancellingCloner creates the voice (billed) and then the client goes away.
type cancellingCloner struct{ cancel context.CancelFunc }

func (c cancellingCloner) Clone(context.Context, string, audiodirector.Sample) (string, error) {
	c.cancel()
	return "billed-voice-1", nil
}

func TestCloneVoice_ABilledVoiceIsStoredEvenWhenTheRequestIsCancelled(t *testing.T) {
	dir := t.TempDir()
	db, err := repository.Open(context.Background(), filepath.Join(dir, "mam.db"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := service.New(service.Deps{DB: db, AssetsDir: filepath.Join(dir, "assets"), Audio: audiodirector.Providers{Cloner: cancellingCloner{cancel: cancel}}})
	p, err := svc.Projects.Create(context.Background(), service.CreateProjectInput{Content: "商品CM"})
	require.NoError(t, err)

	clone, err := svc.Editor.CloneVoice(ctx, p.ID, "ユイ", "", upload("s.mp3", []byte("sample")))

	require.NoError(t, err)
	assert.Equal(t, "billed-voice-1", clone.VoiceID)
	voices, err := svc.Editor.Voices(context.Background(), p.ID)
	require.NoError(t, err)
	require.Len(t, voices, 1)
	assert.Equal(t, "billed-voice-1", voices[0].VoiceID)
}
