package jobhistory_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/repository/jobhistory"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
)

// seeded makes jobs j0..j5, one minute apart, with different kinds, states, codes and Projects.
func seeded(t *testing.T) *jobhistory.Repository {
	t.Helper()
	ctx := context.Background()
	r := jobhistory.New(testdb.Open(t)).WithClock(func() time.Time { return t0 })
	for i, c := range []struct{ kind, state, code, project string }{
		{"generate_shot", "succeeded", "", "p1"},
		{"generate_shot", "failed", "budget_exceeded", "p1"},
		{"judge_edit_timeline", "failed", "internal", "p2"},
		{"export_video", "running", "", "p2"},
		{"generate_audio", "failed", "internal", "p1"},
		{"generate_shot", "queued", "", ""},
	} {
		e := entry(fmt.Sprintf("j%d", i), t0.Add(time.Duration(i)*time.Minute))
		e.Kind, e.ProjectID = c.kind, c.project
		require.NoError(t, r.Insert(ctx, e))
		e.State, e.ErrorCode = c.state, c.code
		require.NoError(t, r.Settle(ctx, e))
	}
	return r
}

func listIDs(t *testing.T, r *jobhistory.Repository, f jobhistory.Filter) []string {
	t.Helper()
	if f.Limit == 0 {
		f.Limit = 100
	}
	page, err := r.List(context.Background(), f)
	require.NoError(t, err)
	var out []string
	for _, e := range page.Entries {
		out = append(out, e.ID)
	}
	return out
}

func TestRepository_ListFiltersNewestFirst(t *testing.T) {
	r := seeded(t)
	for name, tc := range map[string]struct {
		f    jobhistory.Filter
		want []string
	}{
		"all":          {jobhistory.Filter{}, []string{"j5", "j4", "j3", "j2", "j1", "j0"}},
		"kind":         {jobhistory.Filter{Kind: "generate_shot"}, []string{"j5", "j1", "j0"}},
		"state":        {jobhistory.Filter{State: "failed"}, []string{"j4", "j2", "j1"}},
		"error code":   {jobhistory.Filter{ErrorCode: "internal"}, []string{"j4", "j2"}},
		"project":      {jobhistory.Filter{ProjectID: "p2"}, []string{"j3", "j2"}},
		"since":        {jobhistory.Filter{Since: t0.Add(4 * time.Minute)}, []string{"j5", "j4"}},
		"until":        {jobhistory.Filter{Until: t0.Add(2 * time.Minute)}, []string{"j1", "j0"}},
		"range":        {jobhistory.Filter{Since: t0.Add(time.Minute), Until: t0.Add(4 * time.Minute)}, []string{"j3", "j2", "j1"}},
		"combined":     {jobhistory.Filter{State: "failed", ProjectID: "p1", Kind: "generate_audio"}, []string{"j4"}},
		"no match":     {jobhistory.Filter{State: "cancelled"}, nil},
		"project none": {jobhistory.Filter{ProjectID: "nope"}, nil},
	} {
		assert.Equal(t, tc.want, listIDs(t, r, tc.f), name)
	}
}

func TestRepository_ListPagesWithAnOpaqueCursorThatIsStable(t *testing.T) {
	ctx := context.Background()
	r := seeded(t)
	first, err := r.List(ctx, jobhistory.Filter{Limit: 2})
	require.NoError(t, err)
	require.Len(t, first.Entries, 2)
	require.NotEmpty(t, first.Next)
	assert.Equal(t, "j5", first.Entries[0].ID)

	// A job that starts and one that is pruned between the pages do not move them.
	require.NoError(t, r.Insert(ctx, entry("newest", t0.Add(time.Hour))))
	second, err := r.List(ctx, jobhistory.Filter{Limit: 2, After: first.Next})
	require.NoError(t, err)
	assert.Equal(t, []string{"j3", "j2"}, []string{second.Entries[0].ID, second.Entries[1].ID})
	third, err := r.List(ctx, jobhistory.Filter{Limit: 2, After: second.Next})
	require.NoError(t, err)
	assert.Equal(t, []string{"j1", "j0"}, []string{third.Entries[0].ID, third.Entries[1].ID})
	assert.Empty(t, third.Next, "the last page has no cursor")

	exact, err := r.List(ctx, jobhistory.Filter{Limit: 7})
	require.NoError(t, err)
	assert.Len(t, exact.Entries, 7)
	assert.Empty(t, exact.Next, "a page that holds the rest has no cursor")
}

func TestRepository_ListPagesJobsThatStartedAtTheSameInstant(t *testing.T) {
	ctx := context.Background()
	r := jobhistory.New(testdb.Open(t)).WithClock(func() time.Time { return t0 })
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		require.NoError(t, r.Insert(ctx, entry(id, t0)))
	}
	var seen []string
	f := jobhistory.Filter{Limit: 2}
	for range 5 {
		page, err := r.List(ctx, f)
		require.NoError(t, err)
		for _, e := range page.Entries {
			seen = append(seen, e.ID)
		}
		if page.Next == "" {
			break
		}
		f.After = page.Next
	}
	assert.Equal(t, []string{"e", "d", "c", "b", "a"}, seen, "no job twice or skipped")
}

func TestRepository_ListRefusesABadCursorAndAnEmptyPage(t *testing.T) {
	r := seeded(t)
	for _, bad := range []string{"%%%", "bm90LWEtY3Vyc29y" /* "not-a-cursor" */, "MjAyNi0xMC0zMXx4" /* "2026-10-31|x" */} {
		_, err := r.List(context.Background(), jobhistory.Filter{Limit: 1, After: bad})
		assert.ErrorIs(t, err, jobhistory.ErrBadCursor, bad)
	}
	_, err := r.List(context.Background(), jobhistory.Filter{})
	assert.Error(t, err)
}
