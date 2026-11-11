package chatwire_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/chatfix"
	"github.com/ousiassllc/mam/backend/internal/service/chatwire"
)

func png(fill byte) []byte {
	return append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{fill}, 32)...)
}

// Two uploads of the same kind and name that meet are told apart by the file they hold: each gets its own entry, not "the first one that is new".
func TestOwnEntry_TwoUploadsOfTheSameKindAndNameEachGetTheirOwnEntry(t *testing.T) {
	f := chatfix.New(t)
	ctx := context.Background()
	var items []domain.ReferenceItem
	var paths []string
	for _, fill := range []byte{1, 2} {
		data := png(fill)
		view, err := f.Svc.Reference.Upload(ctx, f.ProjectID, service.UploadReferenceInput{
			File: service.Upload{Name: "face.png", Size: int64(len(data)), Open: func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }},
			Kind: "character", Name: "Mio（顔）",
		})
		require.NoError(t, err)
		items = view.Items
	}
	require.Len(t, items, 2)
	for _, it := range items {
		_, abs, err := f.Svc.Reference.StoredAsset(ctx, f.ProjectID, it.AssetID)
		require.NoError(t, err)
		paths = append(paths, abs)
	}

	for i, it := range items {
		got, err := chatwire.OwnEntry(ctx, f.Svc.Reference, f.ProjectID, items, paths[i])

		require.NoError(t, err)
		assert.Equal(t, it.ID, got.ID, "the entry whose file was uploaded")
	}
}
