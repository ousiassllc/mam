package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/logs"
	"github.com/ousiassllc/mam/backend/internal/repository/jobhistory"
)

// fakeStage is a kind whose work ends as the test says: outcomes maps a target to what its Probe reads.
type fakeStage struct{ outcomes map[string]mcpkit.Progress }

func (f *fakeStage) probe(_ context.Context, job mcpkit.JobInfo) (mcpkit.Progress, error) {
	if p, ok := f.outcomes[job.Target]; ok {
		return p, nil
	}
	return mcpkit.Progress{State: mcpkit.JobRunning, Message: "working"}, nil
}

func registerAll(stage *fakeStage) func(*mcptest.Env, *mcpkit.Registrar) {
	return func(env *mcptest.Env, r *mcpkit.Registrar) {
		jobs.Register(r, env.Services)
		logs.Register(r, env.Services)
		r.Jobs.RegisterKind("fake_stage", stage.probe)
	}
}

func list(t *testing.T, env *mcptest.Env, args map[string]any) jobs.ListOutput {
	t.Helper()
	var out jobs.ListOutput
	env.CallOK("list_jobs", args, &out)
	return out
}

func ids(out jobs.ListOutput) []string {
	var s []string
	for _, j := range out.Jobs {
		s = append(s, j.JobID)
	}
	return s
}

var t0 = time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Second)

// history is the table list_jobs reads, to place jobs in time (Jobs.Run uses the clock of the day).
func history(env *mcptest.Env) *jobhistory.Repository { return jobhistory.New(env.DB) }

func put(t *testing.T, env *mcptest.Env, id, kind, project, state, code string, at time.Duration) {
	t.Helper()
	e := jobhistory.Entry{ID: id, Kind: kind, Target: "t-" + id, ProjectID: project, StartedAt: t0.Add(at), State: "queued"}
	require.NoError(t, history(env).Insert(context.Background(), e))
	e.State, e.ErrorCode = state, code
	if state == "failed" {
		e.ErrorMessage = code + ": it failed"
	}
	end := e.StartedAt.Add(time.Second)
	if state != "queued" && state != "running" {
		e.EndedAt = &end
	}
	require.NoError(t, history(env).Settle(context.Background(), e))
}

func seedHistory(t *testing.T, env *mcptest.Env) {
	put(t, env, "j0", "generate_shot", "p1", "succeeded", "", 0)
	put(t, env, "j1", "generate_shot", "p1", "failed", "budget_exceeded", time.Minute)
	put(t, env, "j2", "judge_edit_timeline", "p2", "failed", "internal", 2*time.Minute)
	put(t, env, "j3", "export_video", "p2", "failed", "upstream", 3*time.Minute)
	put(t, env, "j4", "generate_audio", "p1", "failed", "internal", 4*time.Minute)
	put(t, env, "j5", "generate_shot", "", "succeeded", "", 5*time.Minute)
}

func TestListJobs_FiltersByKindStateErrorCodeProjectAndTime(t *testing.T) {
	env := mcptest.New(t, registerAll(&fakeStage{}))
	seedHistory(t, env)

	for name, tc := range map[string]struct {
		args map[string]any
		want []string
	}{
		"newest first": {map[string]any{}, []string{"j5", "j4", "j3", "j2", "j1", "j0"}},
		"kind":         {map[string]any{"kind": "generate_shot"}, []string{"j5", "j1", "j0"}},
		"failed":       {map[string]any{"state": "failed"}, []string{"j4", "j3", "j2", "j1"}},
		"succeeded":    {map[string]any{"state": "succeeded"}, []string{"j5", "j0"}},
		"internal":     {map[string]any{"error_code": "internal"}, []string{"j4", "j2"}},
		"budget":       {map[string]any{"error_code": "budget_exceeded"}, []string{"j1"}},
		"upstream":     {map[string]any{"error_code": "upstream"}, []string{"j3"}},
		"project":      {map[string]any{"project_id": "p2"}, []string{"j3", "j2"}},
		"since":        {map[string]any{"since": t0.Add(4 * time.Minute).Format(time.RFC3339)}, []string{"j5", "j4"}},
		"until":        {map[string]any{"until": t0.Add(2 * time.Minute).Format(time.RFC3339)}, []string{"j1", "j0"}},
		"range":        {map[string]any{"since": t0.Add(time.Minute).Format(time.RFC3339), "until": t0.Add(4 * time.Minute).Format(time.RFC3339)}, []string{"j3", "j2", "j1"}},
		"combined":     {map[string]any{"state": "failed", "error_code": "internal", "project_id": "p1"}, []string{"j4"}},
		"no match":     {map[string]any{"kind": "no_such_kind"}, nil},
	} {
		assert.Equal(t, tc.want, ids(list(t, env, tc.args)), name)
	}
	assert.NotNil(t, list(t, env, map[string]any{"kind": "no_such_kind"}).Jobs, "an empty page is [] not null")

	failed := list(t, env, map[string]any{"error_code": "budget_exceeded"}).Jobs[0]
	assert.Equal(t, "generate_shot", failed.Kind)
	assert.Equal(t, "t-j1", failed.Target)
	assert.Equal(t, "p1", failed.ProjectID)
	assert.Equal(t, mcpkit.JobFailed, failed.State)
	assert.Equal(t, "budget_exceeded: it failed", failed.Error)
	assert.True(t, t0.Add(time.Minute).Equal(failed.StartedAt))
	require.NotNil(t, failed.EndedAt)
	assert.True(t, t0.Add(time.Minute+time.Second).Equal(*failed.EndedAt))
}

func TestListJobs_PagesWithTheCursor(t *testing.T) {
	env := mcptest.New(t, registerAll(&fakeStage{}))
	seedHistory(t, env)

	var seen []string
	args := map[string]any{"limit": 4}
	pages := 0
	for {
		page := list(t, env, args)
		pages++
		seen = append(seen, ids(page)...)
		if page.NextCursor == "" {
			break
		}
		require.Less(t, pages, 5)
		args["cursor"] = page.NextCursor
	}
	assert.Equal(t, 2, pages)
	assert.Equal(t, []string{"j5", "j4", "j3", "j2", "j1", "j0"}, seen)

	filtered := list(t, env, map[string]any{"state": "failed", "limit": 3})
	assert.Equal(t, []string{"j4", "j3", "j2"}, ids(filtered))
	rest := list(t, env, map[string]any{"state": "failed", "limit": 3, "cursor": filtered.NextCursor})
	assert.Equal(t, []string{"j1"}, ids(rest))
	assert.Empty(t, rest.NextCursor)
}

func TestListJobs_RefusesBadInput(t *testing.T) {
	env := mcptest.New(t, registerAll(&fakeStage{}))
	for name, args := range map[string]map[string]any{
		"state":      {"state": "done"},
		"error code": {"error_code": "oops"},
		"since":      {"since": "last week"},
		"until":      {"until": "2026-02-30"},
		"order":      {"since": "2026-10-02T00:00:00Z", "until": "2026-10-01T00:00:00Z"},
		"limit low":  {"limit": -3},
		"limit high": {"limit": jobs.MaxLimit + 1},
		"cursor":     {"cursor": "not-a-cursor"},
	} {
		assert.Contains(t, env.CallError("list_jobs", args), "validation", name)
	}
	list, err := env.Client.ListTools(context.Background(), nil)
	require.NoError(t, err)
	for _, tl := range list.Tools {
		if tl.Name == "list_jobs" {
			assert.True(t, tl.Annotations.ReadOnlyHint)
			return
		}
	}
	t.Fatal("list_jobs is not registered")
}

func TestListJobs_ShowsJobsStartedThroughRunAndSettlesTheOpenOnes(t *testing.T) {
	stage := &fakeStage{outcomes: map[string]mcpkit.Progress{}}
	env := mcptest.New(t, registerAll(stage))
	seedShot(t, env)
	ctx := context.Background()
	start := func(target string) string {
		ref, err := env.Registrar.Jobs.Run(ctx, "fake_stage", target, func(context.Context) (string, error) { return "", nil })
		require.NoError(t, err)
		return ref.JobID
	}
	running, doneJob, failedJob := start("sh1"), start("other"), start("sh1")

	stage.outcomes["other"] = mcpkit.Progress{State: mcpkit.JobSucceeded, Result: map[string]any{"x": 1}}
	stage.outcomes["sh1"] = mcpkit.Progress{State: mcpkit.JobFailed, Error: "conflict: 状態が違います"}
	// Nobody called get_job: the listing looks at the unfinished jobs itself.
	byState := func(state string) []string { return ids(list(t, env, map[string]any{"state": state})) }
	assert.ElementsMatch(t, []string{doneJob}, byState("succeeded"))
	assert.ElementsMatch(t, []string{running, failedJob}, byState("failed"))
	conflicts := list(t, env, map[string]any{"error_code": "conflict", "project_id": "p1"})
	assert.ElementsMatch(t, []string{running, failedJob}, ids(conflicts), "the Project of the Shot target was found")
	for _, j := range conflicts.Jobs {
		assert.Equal(t, "状態が違います", j.Error[len("conflict: "):])
	}

	raw, err := json.Marshal(list(t, env, map[string]any{}))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), `"result"`, "a listing has no results: get_job has them")
}

// The listing of the next process is the listing of this one.
func TestListJobs_ASimulatedRestartKeepsTheListing(t *testing.T) {
	stage := &fakeStage{}
	env := mcptest.New(t, registerAll(stage))
	seedHistory(t, env)
	before := list(t, env, map[string]any{})
	beforeFailed := list(t, env, map[string]any{"state": "failed", "error_code": "internal"})

	after := env.Restart(registerAll(stage))
	assert.Equal(t, before, list(t, after, map[string]any{}))
	assert.Equal(t, beforeFailed, list(t, after, map[string]any{"state": "failed", "error_code": "internal"}))
	assert.Equal(t, []string{"j4", "j2"}, ids(beforeFailed))
}

// An internal failure: list_jobs and get_job give the id, get_app_logs finds the log line of the cause
// with it, and neither the id's message nor the line the agent reads reveals what the cause says to hide.
func TestListJobs_ACorrelationIdLeadsFromTheJobToTheCauseInTheLog(t *testing.T) {
	stage := &fakeStage{outcomes: map[string]mcpkit.Progress{}}
	env := mcptest.New(t, registerAll(stage))
	ctx := context.Background()

	ref, err := env.Registrar.Jobs.Run(ctx, "fake_stage", "tl-1", func(context.Context) (string, error) { return "", nil })
	require.NoError(t, err)
	te := mcpkit.ReportError(env.Logger, "mcp editing stage failed", errors.New("open /srv/assets/x.mp4: permission denied"), "kind", "fake_stage", "job_id", ref.JobID)
	stage.outcomes["tl-1"] = mcpkit.Progress{State: mcpkit.JobFailed, Error: te.Error(), ErrorID: te.ErrorID}

	page := list(t, env, map[string]any{"state": "failed", "error_code": "internal"})
	require.Len(t, page.Jobs, 1)
	j := page.Jobs[0]
	assert.Equal(t, ref.JobID, j.JobID)
	require.NotEmpty(t, j.ErrorID)
	assert.Contains(t, j.Error, j.ErrorID)
	assert.NotContains(t, j.Error, "/srv/assets")
	assert.Equal(t, j.ErrorID, getJob(t, env, ref.JobID).ErrorID, "get_job gives the same id")

	var out logs.Output
	env.CallOK("get_app_logs", map[string]any{"attrs": map[string]any{"error_id": j.ErrorID}}, &out)
	require.Len(t, out.Entries, 1)
	assert.Equal(t, "mcp editing stage failed", out.Entries[0].Message)
	assert.Equal(t, ref.JobID, out.Entries[0].Attrs["job_id"])
	assert.Contains(t, fmt.Sprint(out.Entries[0].Attrs["err"]), "permission denied", "the cause is in the log")

	// The same through a restart.
	after := env.Restart(registerAll(stage))
	again := list(t, after, map[string]any{"error_code": "internal"})
	require.Len(t, again.Jobs, 1)
	assert.Equal(t, j.ErrorID, again.Jobs[0].ErrorID)
	out = logs.Output{}
	after.CallOK("get_app_logs", map[string]any{"attrs": map[string]any{"error_id": j.ErrorID}}, &out)
	assert.Len(t, out.Entries, 1, "the log line is in the same file")
}

// A reason a service wrote into a job must not carry a secret into a listing.
func TestListJobs_NeverReturnsASecretFromAFailureReason(t *testing.T) {
	stage := &fakeStage{outcomes: map[string]mcpkit.Progress{}}
	env := mcptest.New(t, registerAll(stage))
	const token = "mam_mcp_FAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKE0000"
	ctx := context.Background()
	row, err := repositoryJob(env).Create(ctx, domain.JobGenerate, nil)
	require.NoError(t, err)
	_, err = env.Registrar.Jobs.Run(ctx, "fake_stage", "t", func(context.Context) (string, error) { return row.ID, nil })
	require.NoError(t, err)
	require.NoError(t, repositoryJob(env).MarkFailed(ctx, row.ID, "vendor said no: Authorization: Bearer abcdefghijklmnop0000 and key "+token))

	raw, err := json.Marshal(list(t, env, map[string]any{"state": "failed"}))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), token)
	assert.NotContains(t, string(raw), "abcdefghijklmnop0000")
	assert.Contains(t, string(raw), "vendor said no", "the rest of the reason stays")
}
