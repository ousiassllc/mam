package historytest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/history"
)

func runImages(t *testing.T, r *gentesttest.Rig, model string) gentest.BatchStatus {
	t.Helper()
	ref, err := r.Confirmed(gentest.Request{Media: "image", Models: []string{model}, Prompts: []gentest.PromptInput{{Prompt: "a fox"}}})
	require.NoError(t, err)
	return r.Wait(ref.BatchID)
}

// #1079: the history is paged by how many results the filter matches (the same filter as the list, whatever page it asks for).
func TestCountHistoryIsTheSizeOfTheFilteredListWhateverThePage(t *testing.T) {
	r := gentesttest.New(t)
	ctx := context.Background()
	first := runImages(t, r, "flux-2")
	runImages(t, r, "nano-banana")
	require.NoError(t, r.Svc.SetFavorite(ctx, first.Results[0].ID, true))

	count := func(f history.Filter) int {
		n, err := r.Svc.CountHistory(ctx, f)
		require.NoError(t, err)
		return n
	}
	assert.Equal(t, 2, count(history.Filter{}))
	assert.Equal(t, 2, count(history.Filter{Limit: 1, Offset: 1}), "a page does not narrow the count")
	assert.Equal(t, 1, count(history.Filter{Model: "nano-banana"}))
	assert.Equal(t, 2, count(history.Filter{Models: []string{"flux-2", "nano-banana"}}))
	assert.Equal(t, 1, count(history.Filter{FavoriteOnly: true}))
	assert.Equal(t, 0, count(history.Filter{Media: "video"}))
	_, err := r.Svc.CountHistory(ctx, history.Filter{Media: "audio"})
	assert.Error(t, err, "an unknown kind is refused, as the list refuses it")
}
