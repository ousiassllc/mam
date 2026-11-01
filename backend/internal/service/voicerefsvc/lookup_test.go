package voicerefsvc_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
)

func TestForSpeaker_FindsTheCharacterByNameAndItsReferenceVoice(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ref, err := f.svc.SetForProject(ctx, "p1", "c1", voicerefsvc.Input{File: file("a.wav", wav(100)), Text: ptr("やあ")})
	require.NoError(t, err)

	for _, speaker := range []string{"ユイ", " ユイ "} {
		v, err := f.svc.ForSpeaker(ctx, "p1", "", speaker)
		require.NoError(t, err, speaker)
		assert.Equal(t, ref, v.Reference)
		assert.Equal(t, "c1", v.Character.ID)
		assert.Equal(t, filepath.Join(f.assets, filepath.FromSlash(ref.FilePath)), v.Path)
	}

	f.exec(`INSERT INTO character (id, project_id, name) VALUES ('c3', 'p1', 'Mio')`)
	_, err = f.svc.SetForProject(ctx, "p1", "c3", voicerefsvc.Input{File: file("m.wav", wav(100))})
	require.NoError(t, err)
	v, err := f.svc.ForSpeaker(ctx, "p1", "", "mIO")
	require.NoError(t, err)
	assert.Equal(t, "c3", v.Character.ID, "the letter case does not matter")
}

func TestForSpeaker_SaysWhyThereIsNoVoice(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	_, err := f.svc.ForSpeaker(ctx, "p1", "", "誰か")
	assert.ErrorIs(t, err, voicerefsvc.ErrNoCharacter, "no Character of that name")
	_, err = f.svc.ForSpeaker(ctx, "p1", "", "")
	assert.ErrorIs(t, err, voicerefsvc.ErrNoCharacter, "an empty speaker names nobody")
	_, err = f.svc.ForSpeaker(ctx, "p1", "", "ユイ")
	assert.ErrorIs(t, err, voicerefsvc.ErrNoReference, "the Character has no reference voice")
	_, err = f.svc.ForSpeaker(ctx, "nope", "", "ユイ")
	assert.ErrorIs(t, err, voicerefsvc.ErrNotFound)
	_, err = f.svc.ForSpeaker(ctx, "p1", "no-branch", "ユイ")
	assert.ErrorIs(t, err, voicerefsvc.ErrNotFound)

	ref, err := f.svc.SetForProject(ctx, "p1", "c1", voicerefsvc.Input{File: file("a.wav", wav(100))})
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(f.assets, filepath.FromSlash(ref.FilePath))))
	_, err = f.svc.ForSpeaker(ctx, "p1", "", "ユイ")
	assert.ErrorIs(t, err, voicerefsvc.ErrFileMissing, "the voice is set but its file is gone")
}

func TestForSpeaker_SeveralCharactersOfOneName(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.exec(`INSERT INTO character (id, project_id, name) VALUES ('c1b', 'p1', 'ユイ')`)

	// Only one of them has a voice: that one is the voice.
	ref, err := f.svc.SetForProject(ctx, "p1", "c1", voicerefsvc.Input{File: file("a.wav", wav(100))})
	require.NoError(t, err)
	v, err := f.svc.ForSpeaker(ctx, "p1", "", "ユイ")
	require.NoError(t, err)
	assert.Equal(t, "c1", v.Character.ID)

	// Both share one recording: still one voice.
	f.exec(`UPDATE character SET voice_reference = (SELECT voice_reference FROM character WHERE id = 'c1') WHERE id = 'c1b'`)
	v, err = f.svc.ForSpeaker(ctx, "p1", "", "ユイ")
	require.NoError(t, err)
	assert.Equal(t, ref.FilePath, v.Reference.FilePath)

	// Different recordings: the name does not say which, so it is refused instead of picking one.
	_, err = f.svc.SetForProject(ctx, "p1", "c1b", voicerefsvc.Input{File: file("b.wav", wav(100))})
	require.NoError(t, err)
	_, err = f.svc.ForSpeaker(ctx, "p1", "", "ユイ")
	assert.ErrorIs(t, err, voicerefsvc.ErrAmbiguous)
}

func TestForNarration_IsTheCharacterNamedNarration(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	_, err := f.svc.ForNarration(ctx, "p1", "")
	assert.ErrorIs(t, err, voicerefsvc.ErrNoCharacter)

	f.exec(`INSERT INTO character (id, project_id, name) VALUES ('na', 'p1', ?)`, voiceref.NarrationName)
	_, err = f.svc.ForNarration(ctx, "p1", "")
	assert.ErrorIs(t, err, voicerefsvc.ErrNoReference)

	ref, err := f.svc.SetForProject(ctx, "p1", "na", voicerefsvc.Input{File: file("n.wav", wav(100))})
	require.NoError(t, err)
	v, err := f.svc.ForNarration(ctx, "p1", "")
	require.NoError(t, err)
	assert.Equal(t, ref, v.Reference)
}
