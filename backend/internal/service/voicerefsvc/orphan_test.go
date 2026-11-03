package voicerefsvc_test

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
)

// The Character can vanish between the read of the current voice and the write; the new file was stored by then, and is not left behind.
func TestSetForProject_WhenTheCharacterVanishesBeforeTheWriteTheNewFileIsRemoved(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	first, err := f.svc.SetForProject(ctx, "p1", "c1", voicerefsvc.Input{File: file("a.wav", wav(100))})
	require.NoError(t, err)
	vanishing := file("b.wav", wav(120))
	open := vanishing.Open
	vanishing.Open = func() (io.ReadCloser, error) {
		f.exec(`DELETE FROM character WHERE id = 'c1'`) // after the current voice was read, before the row is written
		return open()
	}

	_, err = f.svc.SetForProject(ctx, "p1", "c1", voicerefsvc.Input{File: vanishing})

	require.ErrorIs(t, err, voicerefsvc.ErrNotFound)
	assert.Equal(t, []string{first.FilePath}, f.files(), "the new recording is removed; nothing refers to it")
}
