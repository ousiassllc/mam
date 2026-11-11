package flowtests_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Only the progress card of an install asks how far the job is (the sources look the job up again): a thread of generation cards costs no lookup per card.
func TestProgressCard_OnlyAnInstallAsksHowFarTheJobIs(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "動画")
	jobs := repository.NewJobRepository(f.db)
	generate, err := jobs.Create(ctx, domain.JobGenerate, nil)
	require.NoError(t, err)
	installing, err := jobs.Create(ctx, domain.JobModelInstall, nil)
	require.NoError(t, err)
	var mu sync.Mutex
	asked := map[string]int{}
	f.svc.ChatOps().Progress(func(_ context.Context, id string) string {
		mu.Lock()
		defer mu.Unlock()
		asked[id]++
		return "42%"
	})
	for _, j := range []domain.Job{generate, installing} {
		require.NoError(t, f.svc.Chat.Announce(ctx, p.ID, domain.ChatMessage{Kind: domain.KindProgress, RefID: j.ID, Content: "進捗"}))
	}

	var details = map[string]string{}
	for _, m := range f.thread(t, p.ID) {
		if m.Kind == domain.KindProgress {
			details[m.RefID] = m.Detail
		}
	}

	assert.Equal(t, map[string]string{generate.ID: "", installing.ID: "42%"}, details)
	assert.Zero(t, asked[generate.ID], "a generation's card does not ask")
	assert.Equal(t, 1, asked[installing.ID])
}
