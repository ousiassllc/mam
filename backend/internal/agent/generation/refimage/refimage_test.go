package refimage_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation/refimage"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// assets finds every asset but the ones in missing, and counts the lookups.
type assets struct {
	missing map[string]bool
	err     error
	lookups int
}

func (a *assets) Get(_ context.Context, _, id string) (domain.Asset, error) {
	a.lookups++
	switch {
	case a.err != nil:
		return domain.Asset{}, a.err
	case a.missing[id]:
		return domain.Asset{}, repository.ErrNotFound
	}
	return domain.Asset{ID: id, MimeType: "image/png"}, nil
}

func newReader(a *assets, unreadable map[string]bool, reads *int) *refimage.Reader {
	read := func(asset domain.Asset) ([]byte, error) {
		*reads++
		if unreadable[asset.ID] {
			return nil, errors.New("gone")
		}
		return []byte(asset.ID), nil
	}
	return refimage.New("p", a, read, slog.New(slog.DiscardHandler))
}

func ref(kind domain.ReferenceKind, id string) domain.Reference {
	return domain.Reference{Kind: kind, AssetID: id}
}

func TestReader_ReadsAnImageOnceWhateverItsAnswerWas(t *testing.T) {
	a, reads := &assets{missing: map[string]bool{"lost": true}}, 0
	r := newReader(a, map[string]bool{"broken": true}, &reads)

	for range 3 {
		in, ok, err := r.Input(context.Background(), ref(domain.RefKeyframe, "kf"))
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, "kf", string(in.Data))
		for _, id := range []string{"lost", "broken"} {
			_, ok, err := r.Input(context.Background(), ref(domain.RefCharacter, id))
			require.NoError(t, err)
			assert.False(t, ok, id)
		}
	}

	assert.Equal(t, 3, a.lookups, "one lookup per asset")
	assert.Equal(t, 2, reads, "the missing asset has no file to read; the others are read once")
}

func TestReader_ADatabaseErrorIsReturnedAndAskedAgain(t *testing.T) {
	a, reads := &assets{err: errors.New("database is locked")}, 0
	r := newReader(a, nil, &reads)
	_, _, err := r.Input(context.Background(), ref(domain.RefKeyframe, "kf"))
	require.Error(t, err)

	a.err = nil
	_, ok, err := r.Input(context.Background(), ref(domain.RefKeyframe, "kf"))

	require.NoError(t, err)
	assert.True(t, ok)
}

func TestReader_AttachedDropsAKeyframeThatCannotBeReadOnly(t *testing.T) {
	kf, face := ref(domain.RefKeyframe, "kf"), ref(domain.RefCharacter, "face")
	refs := []domain.Reference{face, kf}
	for name, c := range map[string]struct {
		unreadable map[string]bool
		want       []domain.Reference
	}{
		"read":                  {nil, refs},
		"unreadable":            {map[string]bool{"kf": true}, []domain.Reference{face}},
		"other image unreadble": {map[string]bool{"face": true}, refs},
	} {
		reads := 0
		got, err := newReader(&assets{}, c.unreadable, &reads).Attached(context.Background(), refs)
		require.NoError(t, err, name)
		assert.Equal(t, c.want, got, name)
		assert.Equal(t, []domain.Reference{face, kf}, refs, "the Shot's own list is left as it is: "+name)
	}
	got, err := newReader(&assets{}, nil, new(int)).Attached(context.Background(), []domain.Reference{face})
	require.NoError(t, err)
	assert.Equal(t, []domain.Reference{face}, got, "no Keyframe: nothing to read")
}

func TestReader_InputsStopAtTheLimitAndSkipWhatCannotBeRead(t *testing.T) {
	reads := 0
	r := newReader(&assets{missing: map[string]bool{"b": true}}, map[string]bool{"c": true}, &reads)

	got := r.Inputs(context.Background(), []string{"a", "b", "c", "d", "e"}, 2)

	require.Len(t, got, 2)
	assert.Equal(t, "a", string(got[0].Data))
	assert.Equal(t, "d", string(got[1].Data))
}

func TestWorldAssetIDs_AreThoseOfTheKindThatTheWorldStateHolds(t *testing.T) {
	w := domain.WorldContext{
		Characters: []domain.WorldCharacter{{FaceReferenceAssetID: "face", BodyReferenceAssetID: ""}},
		Locations:  []domain.WorldLocation{{LayoutReferenceAssetID: "layout"}},
	}

	assert.Equal(t, []string{"face"}, refimage.WorldAssetIDs(domain.RefCharacter, w))
	assert.Equal(t, []string{"layout"}, refimage.WorldAssetIDs(domain.RefLocation, w))
	assert.Empty(t, refimage.WorldAssetIDs(domain.RefProduct, w))
	assert.Empty(t, refimage.WorldAssetIDs(domain.RefStyle, w))
}
