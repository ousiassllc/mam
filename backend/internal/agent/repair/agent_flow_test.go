package repair_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/jev"
	"github.com/ousiassllc/mam/backend/internal/agent/repair"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func TestStart_RefusesWhatCannotBeRepaired(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(t *testing.T, f *fx) string // returns the take ID to repair
		wantIs error
	}{
		{"unknown take", func(*testing.T, *fx) string { return "nope" }, repository.ErrNotFound},
		{"take never judged", func(t *testing.T, f *fx) string { f.addTake(t, "t2", "p1/b.mp4"); return "t2" }, repair.ErrNotJudged},
		{"take accepted by the judge", func(t *testing.T, f *fx) string {
			f.addTake(t, "t2", "p1/b.mp4")
			f.judge(t, "t2", domain.JudgeResult{Result: domain.JudgePass})
			return "t2"
		}, repair.ErrAccepted},
		{"shot is being generated", func(t *testing.T, f *fx) string {
			require.NoError(t, f.gens.SetShotState(context.Background(), "sh1", domain.ShotGenerating))
			return "t1"
		}, repair.ErrBusy},
		{"shot is already being repaired", func(t *testing.T, f *fx) string {
			require.NoError(t, f.gens.SetShotState(context.Background(), "sh1", domain.ShotRepairing))
			return "t1"
		}, repair.ErrBusy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFx(t)
			id := tt.setup(t, f)

			_, err := f.agent.Start(context.Background(), id, false)

			require.ErrorIs(t, err, tt.wantIs)
			assert.Empty(t, f.jobs.enqueued(), "no job for a repair that is refused")
		})
	}
}

func TestStart_ClaimsTheShotQueuesTheJobAndPostsTheProgressCard(t *testing.T) {
	f := newFx(t)

	job, err := f.agent.Start(context.Background(), "t1", true)
	require.NoError(t, err)

	assert.Equal(t, domain.JobRepair, job.Kind)
	var p map[string]any
	require.NoError(t, json.Unmarshal(job.Payload, &p))
	assert.Equal(t, "t1", p["take_id"])
	assert.Equal(t, "sh1", p["shot_id"])
	assert.Equal(t, f.judged.ID, p["judge_result_id"])
	assert.Equal(t, true, p["auto"])
	assert.Equal(t, domain.ShotRepairing, f.shotState(t))
	f.chat.mu.Lock()
	require.Len(t, f.chat.announced, 1)
	assert.Equal(t, domain.KindProgress, f.chat.announced[0].Kind)
	assert.Equal(t, job.ID, f.chat.announced[0].RefID)
	f.chat.mu.Unlock()
	assert.Equal(t, []generation.Stage{generation.StageQueued}, f.progress.stages())

	_, err = f.agent.Start(context.Background(), "t1", false)
	assert.ErrorIs(t, err, repair.ErrBusy, "no double execution while the first repair owns the shot")
}

func TestStart_ALostJobGivesTheShotBack(t *testing.T) {
	f := newFx(t)
	f.jobs.err = errors.New("queue is closed")

	_, err := f.agent.Start(context.Background(), "t1", false)

	require.ErrorContains(t, err, "enqueue repair job")
	assert.Equal(t, domain.ShotPlanning, f.shotState(t), "the shot is not left REPAIRING")
}

func TestStart_AFailedChatPostDoesNotStopTheRepair(t *testing.T) {
	f := newFx(t)
	f.chat.announceErr = errors.New("chat down")

	_, err := f.agent.Start(context.Background(), "t1", false)

	require.NoError(t, err)
	assert.Contains(t, f.logs.String(), "post repair progress card failed")
	assert.Len(t, f.jobs.enqueued(), 1)
}

func TestHandle_RejectsAJobWithoutAUsablePayload(t *testing.T) {
	f := newFx(t)
	for name, payload := range map[string]string{
		"not json":          `nope`,
		"no take":           `{"shot_id":"sh1","judge_result_id":"j"}`,
		"no shot":           `{"take_id":"t1","judge_result_id":"j"}`,
		"no judge, partial": `{"take_id":"t1","shot_id":"sh1"}`,
	} {
		t.Run(name, func(t *testing.T) {
			err := f.agent.Handle(context.Background(), domain.Job{ID: "j1", Kind: domain.JobRepair, Payload: json.RawMessage(payload)})
			assert.ErrorContains(t, err, "invalid payload")
		})
	}
	assert.Empty(t, f.regen.regenCalls)
}

func TestHandle_MakesANewTakeWithTheStrategyTheRepairAgentChose(t *testing.T) {
	f := newFx(t)
	f.llm.text = answer(domain.RepairPrompt, "Reduce Camera Speed", "slow_pan")
	// The Judge recommended an instruction of its own and named what to keep.
	f.judged = f.judge(t, "t1", domain.JudgeResult{
		Result: domain.JudgeFail, Issues: []domain.JudgeIssue{{Type: "camera_speed", Severity: domain.SeverityMedium}},
		Preserve:       []string{"character"},
		RepairStrategy: &domain.RepairStrategy{Action: domain.RepairFullRetake, Instructions: []string{"slow_pan"}},
	})

	require.NoError(t, f.run(t, false))

	require.Len(t, f.regen.regenCalls, 1)
	got := f.regen.regenCalls[0]
	assert.Equal(t, domain.RepairPrompt, got.Action)
	assert.Equal(t, []string{"slow_pan", "reduce_camera_speed"}, got.Instructions, "the Judge's instructions first, the agent's after, no repeats")
	assert.Equal(t, []string{"character"}, got.Preserve)
	assert.Equal(t, domain.ModelName("kling-3.0"), got.SourceModel)
	assert.Equal(t, f.judged.Issues, got.Issues)
	assert.Empty(t, got.Ranges, "no time ranges: no splice")

	history := f.history(t)
	require.Len(t, history, 1)
	assert.Equal(t, domain.RepairPrompt, history[0].Action)
	assert.Equal(t, "new-take-1", history[0].ResultingTakeID, "the repair is traced to the Take it made")
	assert.Equal(t, domain.DecidedByLLM, history[0].Instructions.DecidedBy)
	assert.Equal(t, "because "+string(domain.RepairPrompt), history[0].Instructions.Reason)
	assert.Equal(t, "t1", history[0].Instructions.SourceTakeID)
	assert.Equal(t, 1, domain.RetriesDone(history))

	assert.Equal(t, domain.ShotPlanning, f.shotState(t), "the shot is given back")
	assert.Contains(t, f.chat.texts()[len(f.chat.texts())-1], "再試行 1/2 回目")
	assert.Equal(t, generation.StageDone, f.progress.last().Stage)
	assert.Empty(t, f.chat.escalations)
}

func TestHandle_TheLoopContinuesAfterTheNewTakeOnceTheShotIsFree(t *testing.T) {
	f := newFx(t)

	require.NoError(t, f.run(t, true))

	// new-take-1 is not judged yet, so the loop has nothing to repair; nothing is queued twice.
	assert.Len(t, f.jobs.enqueued(), 1)

	// Once it is judged and fails too, the next look of the loop repairs the best take again.
	f.judge(t, "new-take-1", failing(domain.JudgeIssue{Type: "flicker", Severity: domain.SeverityMedium}))
	f.agent.Continue(context.Background(), "sh1")
	require.Len(t, f.jobs.enqueued(), 2)
	assert.Equal(t, domain.ShotRepairing, f.shotState(t))
}

func TestHandle_AtTheRetryLimitTheUserIsAskedInsteadOfMakingATake(t *testing.T) {
	f := newFx(t) // balanced: 2 retries
	f.usedRetries(t, 2)

	require.NoError(t, f.run(t, false))

	assert.Empty(t, f.regen.regenCalls, "no Take is made past the limit")
	assert.Empty(t, f.regen.strategyCalls, "the limit is decided before any cost is estimated")
	require.Len(t, f.chat.escalations, 1)
	esc := f.chat.escalations[0]
	assert.Equal(t, "p1", esc.ProjectID)
	assert.Equal(t, "sh1", esc.ShotID)
	assert.Contains(t, esc.Text, domain.RetryMaxRetries.Reason())
	assert.Contains(t, esc.Text, "camera_speed[medium]", "the card shows the latest Judge result")
	assert.Len(t, esc.Options, 3)
	assert.Len(t, f.history(t), 2, "no repair action is recorded for a repair that did not start")
	assert.Equal(t, domain.ShotPlanning, f.shotState(t))
	assert.Contains(t, f.progress.stages(), generation.StageDone)
}

func TestHandle_ARaisedRetryLimitAllowsAnotherRetry(t *testing.T) {
	f := newFx(t)
	f.usedRetries(t, 2)
	execAll(t, f.db, `INSERT INTO project_policy (project_id, max_retries) VALUES ('p1', 3)`)

	require.NoError(t, f.run(t, false))

	assert.Len(t, f.regen.regenCalls, 1)
	assert.Empty(t, f.chat.escalations)
	assert.Contains(t, f.chat.texts()[len(f.chat.texts())-1], "再試行 3/3 回目")
}

func TestHandle_AnEscalationIsNotRepeatedByTheLoopButAManualRepairAsksAgain(t *testing.T) {
	f := newFx(t)
	f.usedRetries(t, 2)

	require.NoError(t, f.run(t, true))
	require.NoError(t, f.run(t, true))
	assert.Len(t, f.chat.escalations, 1, "the automatic loop asks once per Judge result")

	require.NoError(t, f.run(t, false))
	assert.Len(t, f.chat.escalations, 2, "a Repair the user asked for always answers")
}

func TestHandle_TheBudgetStopsTheLoopAndAsksTheUser(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, f *fx)
		verdict domain.RetryVerdict
	}{
		{"not even one take fits the project budget", func(_ *testing.T, f *fx) {
			f.regen.strategyErr = map[domain.RepairAction]error{domain.RepairFullRetake: generation.ErrBudgetExceeded}
		}, domain.RetryProjectBudget},
		{"not even one take fits the shot budget", func(t *testing.T, f *fx) {
			f.setProject(t, `shot_budget_ceiling = 3`)
			f.regen.strategyErr = map[domain.RepairAction]error{domain.RepairFullRetake: generation.ErrBudgetExceeded}
		}, domain.RetryShotBudget},
		{"the shot has spent its ceiling", func(t *testing.T, f *fx) {
			f.setProject(t, `shot_budget_ceiling = 10`)
			f.regen.cost = map[domain.RepairAction]float64{domain.RepairFullRetake: 2}
			execAll(t, f.db, `INSERT INTO cost_ledger_entry (id, project_id, shot_id, provider, estimated_cost) VALUES ('c1', 'p1', 'sh1', 'kling', 9)`)
		}, domain.RetryShotBudget},
		{"the project has spent its ceiling", func(t *testing.T, f *fx) {
			f.setProject(t, `project_budget_ceiling = 10`)
			f.regen.cost = map[domain.RepairAction]float64{domain.RepairFullRetake: 2}
			execAll(t, f.db, `INSERT INTO cost_ledger_entry (id, project_id, provider, estimated_cost, actual_cost) VALUES ('c1', 'p1', 'kling', 1, 9)`)
		}, domain.RetryProjectBudget},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFx(t)
			tt.setup(t, f)

			require.NoError(t, f.run(t, false))

			assert.Empty(t, f.regen.regenCalls)
			require.Len(t, f.chat.escalations, 1)
			assert.Contains(t, f.chat.escalations[0].Text, tt.verdict.Reason())
			assert.Empty(t, f.history(t))
			assert.Equal(t, domain.ShotPlanning, f.shotState(t))
		})
	}
}

func TestHandle_TheBudgetStillHoldsForAStrategyThatCostsMoreThanAFullRetake(t *testing.T) {
	f := newFx(t)
	f.setProject(t, `shot_budget_ceiling = 4`)
	f.regen.cost = map[domain.RepairAction]float64{domain.RepairFullRetake: 1, domain.RepairDifferentModel: 5}
	f.llm.text = answer(domain.RepairDifferentModel)

	require.NoError(t, f.run(t, false))

	assert.Empty(t, f.regen.regenCalls, "the other model would not fit: nothing is generated")
	require.Len(t, f.chat.escalations, 1)
	assert.Contains(t, f.chat.escalations[0].Text, domain.RetryShotBudget.Reason())
	assert.Empty(t, f.history(t), "the strategy is recorded only once it is allowed")
}

func TestHandle_WithinTheBudgetTheRetryGoesThrough(t *testing.T) {
	f := newFx(t)
	f.setProject(t, `shot_budget_ceiling = 10, project_budget_ceiling = 100`)
	f.regen.cost = map[domain.RepairAction]float64{domain.RepairFullRetake: 2}
	execAll(t, f.db, `INSERT INTO cost_ledger_entry (id, project_id, shot_id, provider, estimated_cost) VALUES ('c1', 'p1', 'sh1', 'kling', 8)`)

	require.NoError(t, f.run(t, false))

	assert.Len(t, f.regen.regenCalls, 1, "8 + 2 is exactly the ceiling: still allowed")
	assert.Empty(t, f.chat.escalations)
}

func TestHandle_AProviderFailureFailsTheRepairAndTellsTheUser(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"provider error", errProvider, generation.FailureReason(errProvider)},
		{"no shot spec", generation.ErrNoSpec, repair.FailureReason(generation.ErrNoSpec)},
		{"budget", generation.ErrBudgetExceeded, generation.FailureReason(generation.ErrBudgetExceeded)},
		{"locked parameter", repository.ErrLocked, repair.FailureReason(repository.ErrLocked)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFx(t)
			f.regen.regenErr = tt.err

			err := f.run(t, false)

			require.EqualError(t, err, tt.want, "the job record carries the user-facing reason, not provider details")
			assert.NotContains(t, err.Error(), "exploded")
			last := f.chat.texts()[len(f.chat.texts())-1]
			assert.Contains(t, last, "Repair に失敗しました")
			assert.Contains(t, last, tt.want)
			assert.Equal(t, generation.StageFailed, f.progress.last().Stage)
			assert.Equal(t, tt.want, f.progress.last().Message)
			assert.Equal(t, domain.ShotPlanning, f.shotState(t), "the shot is released for the next attempt")

			history := f.history(t)
			require.Len(t, history, 1, "the action was recorded before the provider was asked")
			assert.Empty(t, history[0].ResultingTakeID)
			assert.Zero(t, domain.RetriesDone(history), "a repair that made no Take costs no retry")
		})
	}
}

func TestHandle_AStrategyEstimateFailureFailsBeforeAnythingIsRecorded(t *testing.T) {
	f := newFx(t)
	f.regen.strategyErr = map[domain.RepairAction]error{domain.RepairFullRetake: generation.ErrNoSpec}

	err := f.run(t, false)

	require.EqualError(t, err, repair.FailureReason(generation.ErrNoSpec))
	assert.Empty(t, f.regen.regenCalls)
	assert.Empty(t, f.history(t))
	assert.Empty(t, f.chat.escalations, "a failure is not a policy stop")
	assert.Equal(t, domain.ShotPlanning, f.shotState(t))
}

func TestHandle_AMissingTakeFailsQuietly(t *testing.T) {
	f := newFx(t)
	_, err := f.agent.Start(context.Background(), "t1", false)
	require.NoError(t, err)
	payload, _ := json.Marshal(map[string]string{"take_id": "ghost", "shot_id": "sh1", "judge_result_id": f.judged.ID})

	err = f.agent.Handle(context.Background(), domain.Job{ID: "j", Kind: domain.JobRepair, Payload: payload})

	require.ErrorIs(t, err, repository.ErrNotFound)
	assert.Len(t, f.chat.texts(), 1, "only the progress card of Start: nobody is told about a vanished take")
	assert.Equal(t, domain.ShotPlanning, f.shotState(t))
}

func TestHandle_CancellationStopsTheRepairWithoutBlamingTheProviderAndReleasesTheShot(t *testing.T) {
	f := newFx(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.regen.beforeRegenerate = cancel
	f.regen.regenErr = context.Canceled
	job, err := f.agent.Start(ctx, "t1", false)
	require.NoError(t, err)

	err = f.agent.Handle(ctx, job)

	require.ErrorIs(t, err, context.Canceled)
	assert.Len(t, f.chat.texts(), 1, "shutting down: no failure notice")
	assert.NotContains(t, f.progress.stages(), generation.StageFailed)
	assert.Equal(t, domain.ShotPlanning, f.shotState(t), "released on a context that survives the shutdown")
	assert.Zero(t, domain.RetriesDone(f.history(t)))
}

func TestHandle_ACancelledContextDoesNotStartTheRepair(t *testing.T) {
	f := newFx(t)
	job, err := f.agent.Start(context.Background(), "t1", false)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = f.agent.Handle(ctx, job)

	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, f.regen.regenCalls)
}

func TestHandle_EditLevelRepairMakesNoTakeAndNamesTheRanges(t *testing.T) {
	f := newFx(t)
	f.judged = f.judge(t, "t1", failing(domain.JudgeIssue{Type: "flicker", Severity: domain.SeverityLow, TimeRange: "1.0-1.5s"}))
	f.llm.text = answer(domain.RepairEditLevel)

	require.NoError(t, f.run(t, false))

	assert.Empty(t, f.regen.regenCalls)
	last := f.chat.texts()[len(f.chat.texts())-1]
	assert.Contains(t, last, "Edit-Level Repair")
	assert.Contains(t, last, "1.0-1.5s（flicker）")
	history := f.history(t)
	require.Len(t, history, 1)
	assert.Equal(t, domain.RepairEditLevel, history[0].Action)
	assert.Zero(t, domain.RetriesDone(history), "Edit-Level Repair is no retry")
	assert.Equal(t, domain.ShotPlanning, f.shotState(t))
	assert.Len(t, f.jobs.enqueued(), 1, "no new Take: nothing to judge and nothing to continue")
}

func TestHandle_EditLevelRepairSelectsTheTakeSoThatTheAutoEditingChainCanGoOn(t *testing.T) {
	editable := failing(domain.JudgeIssue{Type: "flicker", Severity: domain.SeverityLow, TimeRange: "1.0-1.5s"})
	tests := []struct {
		name         string
		setup        func(t *testing.T, f *fx)
		wantSelected string
		wantNotice   bool
	}{
		{"AUTO: the kept Take becomes the Selected Take", func(*testing.T, *fx) {}, "t1", true},
		{"DIRECTOR: the director chooses", func(t *testing.T, f *fx) { f.setProject(t, "mode = 'director'") }, "", false},
		{"Take Selection MANUAL: the AI keeps its hands off", func(t *testing.T, f *fx) {
			execAll(t, f.db, fmt.Sprintf(`INSERT INTO lock (id, project_id, target_type, target_id, parameter_name, mode) VALUES ('l1', 'p1', '%s', 'sh1', '%s', 'manual')`, domain.LockTargetShot, domain.ParamTakeSelection))
		}, "", false},
		{"a Take chosen earlier stands", func(t *testing.T, f *fx) {
			f.addTake(t, "chosen", "p1/chosen.mp4")
			execAll(t, f.db, `UPDATE shot SET selected_take_id = 'chosen' WHERE id = 'sh1'`)
		}, "chosen", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFx(t)
			tc.setup(t, f)
			f.judged = f.judge(t, "t1", editable)
			f.llm.text = answer(domain.RepairEditLevel)

			require.NoError(t, f.run(t, true))

			ref, err := f.gens.GetShot(context.Background(), "sh1")
			require.NoError(t, err)
			assert.Equal(t, tc.wantSelected, ref.Shot.SelectedTakeID)
			assert.Equal(t, tc.wantNotice, strings.Contains(f.chat.texts()[len(f.chat.texts())-1], "Rough Cut に載せます"))
		})
	}
}

func TestHandle_WhenTheRepairAgentCannotAnswerTheFallbackIsUsed(t *testing.T) {
	tests := []struct {
		name   string
		llm    func(f *fx)
		want   domain.RepairAction
		reason string
	}{
		{"provider error: the Judge's recommendation", func(f *fx) { f.llm.err = errors.New("rate limited") }, domain.RepairDifferentModel, "Judge's recommendation"},
		{"not a candidate: full retake", func(f *fx) {
			f.llm.text = answer(domain.RepairEditLevel)
			f.judged = f.judge(t, "t1", failing(domain.JudgeIssue{Type: "camera_speed", Severity: domain.SeverityMedium}))
		}, domain.RepairFullRetake, "full retake"},
		{"not json: full retake", func(f *fx) {
			f.llm.text = "I think we should retry"
			f.judged = f.judge(t, "t1", failing(domain.JudgeIssue{Type: "camera_speed", Severity: domain.SeverityMedium}))
		}, domain.RepairFullRetake, "full retake"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFx(t)
			f.judged = f.judge(t, "t1", domain.JudgeResult{
				Result: domain.JudgeFail, Issues: []domain.JudgeIssue{{Type: "camera_speed", Severity: domain.SeverityMedium}},
				RepairStrategy: &domain.RepairStrategy{Action: domain.RepairDifferentModel},
			})
			tt.llm(f)

			require.NoError(t, f.run(t, false))

			history := f.history(t)
			require.Len(t, history, 1)
			assert.Equal(t, tt.want, history[0].Action)
			assert.Equal(t, domain.DecidedByRule, history[0].Instructions.DecidedBy)
			assert.Contains(t, history[0].Instructions.Reason, tt.reason)
			assert.Len(t, f.regen.regenCalls, 1, "the repair goes on")
		})
	}
}

func TestHandle_DifferentModelIsNotACandidateWhenThereIsNoOtherModel(t *testing.T) {
	f := newFx(t)
	f.regen.strategyErr = map[domain.RepairAction]error{domain.RepairDifferentModel: generation.ErrNoOtherModel}
	f.llm.text = answer(domain.RepairDifferentModel)

	require.NoError(t, f.run(t, false))

	assert.Equal(t, domain.RepairFullRetake, f.history(t)[0].Action, "an answer outside the candidates is never used")
}

func TestDecide(t *testing.T) {
	open := repair.Situation{
		Result:        domain.JudgeResult{Result: domain.JudgeFail, Issues: []domain.JudgeIssue{{Type: "camera_speed", Severity: domain.SeverityMedium}}},
		HasReferences: true, OtherModel: true,
	}
	drift := open
	drift.Result.Issues = []domain.JudgeIssue{{Type: domain.IssueTypeFaceIdentityDrift, Severity: domain.SeverityHigh}}
	recommended := open
	recommended.Result.RepairStrategy = &domain.RepairStrategy{Action: domain.RepairPrompt}
	all := repair.Candidates(open)

	tests := []struct {
		name       string
		s          repair.Situation
		candidates []domain.RepairAction
		llmText    string
		llmErr     error
		noLLM      bool
		cancelled  bool
		wantAction domain.RepairAction
		wantBy     domain.RepairDecider
		wantExtra  []string
		wantLLM    int
	}{
		{name: "one candidate left", s: open, candidates: []domain.RepairAction{domain.RepairFullRetake}, wantAction: domain.RepairFullRetake, wantBy: domain.DecidedByRule},
		{name: "face identity drift goes to reference reinforcement", s: drift, candidates: repair.Candidates(drift), wantAction: domain.RepairReferenceReinforce, wantBy: domain.DecidedByRule},
		{name: "the agent chooses", s: open, candidates: all, llmText: answer(domain.RepairDifferentModel, "Try Other"), wantAction: domain.RepairDifferentModel, wantBy: domain.DecidedByLLM, wantExtra: []string{"try_other"}, wantLLM: 1},
		{name: "no LLM configured", s: recommended, candidates: all, noLLM: true, wantAction: domain.RepairPrompt, wantBy: domain.DecidedByRule},
		{name: "provider failure", s: open, candidates: all, llmErr: errors.New("boom"), wantAction: domain.RepairFullRetake, wantBy: domain.DecidedByRule, wantLLM: 1},
		{name: "cancelled while asking", s: recommended, candidates: all, llmErr: context.Canceled, cancelled: true, wantAction: domain.RepairPrompt, wantBy: domain.DecidedByRule, wantLLM: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := []fxOption{}
			if tt.noLLM {
				opts = append(opts, withoutLLM())
			}
			f := newFx(t, opts...)
			f.llm.text, f.llm.err = tt.llmText, tt.llmErr
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancelled {
				cancel()
			}

			action, reason, by, extra := f.agent.Decide(ctx, tt.s, tt.candidates)

			assert.Equal(t, tt.wantAction, action)
			assert.Equal(t, tt.wantBy, by)
			assert.NotEmpty(t, reason)
			assert.Equal(t, tt.wantExtra, extra)
			assert.Equal(t, tt.wantLLM, f.llm.calls)
			if tt.cancelled {
				assert.NotContains(t, f.logs.String(), "could not choose", "a shutdown is not a Repair Agent failure")
			}
			if tt.llmErr != nil && !tt.cancelled {
				assert.Contains(t, f.logs.String(), "could not choose")
			}
		})
	}
}

func TestHandle_PartialRegenerationSplicesTheFixedRangesIntoTheFailedTake(t *testing.T) {
	judgeErr := errors.New("judge queue full")
	tests := []struct {
		name       string
		splicer    *fakeSplicer
		judge      *fakeTakeJudge
		wantNotice string
		wantJudged []string
	}{
		{"spliced and judged", &fakeSplicer{id: "spliced-1"}, &fakeTakeJudge{}, "つなぎ込みました", []string{"spliced-1"}},
		{"splice fails: the regenerated take stays", &fakeSplicer{err: errors.New("ffmpeg crashed")}, &fakeTakeJudge{}, "つなぎ込めませんでした（ffmpeg crashed）", nil},
		{"splice not worth making", &fakeSplicer{}, &fakeTakeJudge{}, "つなぎ込まず", nil},
		{"the judge of the spliced take cannot start", &fakeSplicer{id: "spliced-1"}, &fakeTakeJudge{err: judgeErr}, "つなぎ込みました", []string{"spliced-1"}},
		{"no judge wired", &fakeSplicer{id: "spliced-1"}, nil, "つなぎ込みました", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFx(t, withSplicer(tt.splicer, tt.judge))
			f.judged = f.judge(t, "t1", failing(
				domain.JudgeIssue{Type: "flicker", Severity: domain.SeverityMedium, TimeRange: "1.0-1.8s"},
				domain.JudgeIssue{Type: "artifact", Severity: domain.SeverityMedium, TimeRange: "2.5-3.0s"},
			))
			f.llm.text = answer(domain.RepairPartialRegeneration)

			require.NoError(t, f.run(t, false), "a splice problem never fails the repair")

			want := []domain.TimeRange{{From: 1.0, To: 1.8}, {From: 2.5, To: 3.0}}
			require.Len(t, f.regen.regenCalls, 1)
			assert.Equal(t, want, f.regen.regenCalls[0].Ranges)
			require.Len(t, tt.splicer.calls, 1)
			assert.Equal(t, splicerCall{"sh1", "t1", "new-take-1", want}, tt.splicer.calls[0])
			texts := f.chat.texts()
			assert.Contains(t, texts[len(texts)-2], tt.wantNotice, "the splice notice comes before the retry notice")
			if tt.judge != nil {
				assert.Equal(t, tt.wantJudged, tt.judge.started)
			}
			if errors.Is(tt.judge.errOrNil(), judgeErr) {
				assert.Contains(t, f.logs.String(), "start judge of spliced take failed")
			}
			assert.Equal(t, 1, domain.RetriesDone(f.history(t)), "the regenerated Take counts as the retry")
		})
	}
}

func (j *fakeTakeJudge) errOrNil() error {
	if j == nil {
		return nil
	}
	return j.err
}

func TestHandle_PartialRegenerationWithoutASplicerRegeneratesTheWholeShot(t *testing.T) {
	f := newFx(t) // no Splicer
	f.judged = f.judge(t, "t1", failing(domain.JudgeIssue{Type: "flicker", Severity: domain.SeverityMedium, TimeRange: "1.0-1.8s"}))
	f.llm.text = answer(domain.RepairPartialRegeneration)

	require.NoError(t, f.run(t, false))

	require.Len(t, f.regen.regenCalls, 1)
	assert.Equal(t, domain.RepairPartialRegeneration, f.regen.regenCalls[0].Action)
	assert.Empty(t, f.regen.regenCalls[0].Ranges)
}

func TestHandle_PartialRegenerationWithAnIssueWithoutARangeIsNotSpliced(t *testing.T) {
	splicer := &fakeSplicer{id: "spliced-1"}
	f := newFx(t, withSplicer(splicer, &fakeTakeJudge{}))
	f.judged = f.judge(t, "t1", failing(
		domain.JudgeIssue{Type: "flicker", Severity: domain.SeverityMedium, TimeRange: "1.0-1.8s"},
		domain.JudgeIssue{Type: "camera_speed", Severity: domain.SeverityMedium},
	))
	f.llm.text = answer(domain.RepairPartialRegeneration)

	require.NoError(t, f.run(t, false))

	assert.Empty(t, splicer.calls, "one Issue covers the whole Shot: only a whole new Take can fix it")
}

func TestHandle_TheShadowIsMeasuredButNeverObeyed(t *testing.T) {
	tests := []struct {
		name       string
		shadow     *fakeShadow
		llmText    string
		wantRuns   int
		wantErrors int
		wantAgreed int
		wantCalls  int
	}{
		{"agrees", &fakeShadow{choice: jev.Choice{Action: domain.RepairPrompt, Confidence: 0.9, Model: "jev-1"}}, answer(domain.RepairPrompt), 1, 0, 1, 1},
		{"disagrees", &fakeShadow{choice: jev.Choice{Action: domain.RepairFullRetake, Confidence: 0.4}}, answer(domain.RepairPrompt), 1, 0, 0, 1},
		{"fails", &fakeShadow{err: errors.New("jev status 500")}, answer(domain.RepairPrompt), 1, 1, 0, 1},
		{"not set up", &fakeShadow{err: jev.ErrNoAPIKey}, answer(domain.RepairPrompt), 0, 0, 0, 1},
		{"rule decided: nothing to compare", &fakeShadow{choice: jev.Choice{Action: domain.RepairPrompt}}, "", 0, 0, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFx(t, withShadow(tt.shadow))
			if tt.llmText == "" { // a face identity drift leaves the Repair Agent no choice
				execAll(t, f.db, `UPDATE shot SET shot_spec = '{"references":[{"kind":"character","asset_id":"a","prompt":"p","model":"kling-3.0"}]}' WHERE id = 'sh1'`)
				f.judged = f.judge(t, "t1", failing(domain.JudgeIssue{Type: domain.IssueTypeFaceIdentityDrift, Severity: domain.SeverityHigh}))
			}
			f.llm.text = tt.llmText

			require.NoError(t, f.run(t, false), "whatever the shadow does, the repair succeeds")

			assert.Equal(t, tt.wantCalls, tt.shadow.calls)
			report, err := f.repairs.ShadowReport(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tt.wantRuns, report.Runs)
			assert.Equal(t, tt.wantErrors, report.Errors)
			assert.Equal(t, tt.wantAgreed, report.Agreed)
			assert.Len(t, f.regen.regenCalls, 1)
		})
	}
}

func TestStartPartial_RefusesWhatCannotBeRegeneratedByRange(t *testing.T) {
	ranges := []domain.TimeRange{{From: 1, To: 2}}
	tests := []struct {
		name    string
		splicer bool
		setup   func(t *testing.T, f *fx)
		take    string
		req     repair.PartialRequest
		check   func(t *testing.T, err error)
	}{
		{name: "no ffmpeg", take: "t1", req: repair.PartialRequest{Ranges: ranges},
			check: func(t *testing.T, err error) { assert.ErrorIs(t, err, repair.ErrSpliceUnavailable) }},
		{name: "no range", splicer: true, take: "t1",
			check: func(t *testing.T, err error) { assert.ErrorContains(t, err, "needs a time range") }},
		{name: "unknown take", splicer: true, take: "ghost", req: repair.PartialRequest{Ranges: ranges},
			check: func(t *testing.T, err error) { assert.ErrorIs(t, err, repository.ErrNotFound) }},
		{name: "take without a file", splicer: true, take: "t2", req: repair.PartialRequest{Ranges: ranges},
			setup: func(t *testing.T, f *fx) { f.addTake(t, "t2", "") },
			check: func(t *testing.T, err error) { assert.ErrorContains(t, err, "no file") }},
		{name: "prompt is LOCKed", splicer: true, take: "t1", req: repair.PartialRequest{Ranges: ranges},
			setup: func(t *testing.T, f *fx) {
				execAll(t, f.db, fmt.Sprintf(`INSERT INTO lock (id, project_id, target_type, target_id, parameter_name, mode) VALUES ('l1', 'p1', '%s', 'sh1', '%s', 'lock')`, domain.LockTargetShot, domain.ParamPrompt))
			},
			check: func(t *testing.T, err error) {
				var locked *repository.LockedParametersError
				require.ErrorAs(t, err, &locked)
				assert.Equal(t, []domain.LockParameter{domain.ParamPrompt}, locked.Parameters)
			}},
		{name: "budget exceeded", splicer: true, take: "t1", req: repair.PartialRequest{Ranges: ranges},
			setup: func(_ *testing.T, f *fx) {
				f.regen.strategyErr = map[domain.RepairAction]error{domain.RepairPartialRegeneration: generation.ErrBudgetExceeded}
			},
			check: func(t *testing.T, err error) { assert.ErrorIs(t, err, generation.ErrBudgetExceeded) }},
		{name: "shot busy", splicer: true, take: "t1", req: repair.PartialRequest{Ranges: ranges},
			setup: func(t *testing.T, f *fx) {
				require.NoError(t, f.gens.SetShotState(context.Background(), "sh1", domain.ShotGenerating))
			},
			check: func(t *testing.T, err error) { assert.ErrorIs(t, err, repair.ErrBusy) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var opts []fxOption
			if tt.splicer {
				opts = append(opts, withSplicer(&fakeSplicer{}, &fakeTakeJudge{}))
			}
			f := newFx(t, opts...)
			if tt.setup != nil {
				tt.setup(t, f)
			}
			before := f.shotState(t)

			_, err := f.agent.StartPartial(context.Background(), tt.take, tt.req)

			require.Error(t, err)
			tt.check(t, err)
			assert.Empty(t, f.jobs.enqueued())
			assert.Equal(t, before, f.shotState(t), "a refused request leaves the shot as it was")
		})
	}
}

func TestStartPartial_ALostJobGivesTheShotBack(t *testing.T) {
	f := newFx(t, withSplicer(&fakeSplicer{}, nil))
	f.jobs.err = errors.New("queue is closed")

	_, err := f.agent.StartPartial(context.Background(), "t1", repair.PartialRequest{Ranges: []domain.TimeRange{{From: 1, To: 2}}})

	require.ErrorContains(t, err, "enqueue partial regeneration job")
	assert.Equal(t, domain.ShotPlanning, f.shotState(t))
}

func TestStartPartial_TheDirectorsRangesAreRegeneratedAndSplicedWithoutTheRetryPolicy(t *testing.T) {
	splicer, judge := &fakeSplicer{id: "spliced-1"}, &fakeTakeJudge{}
	f := newFx(t, withSplicer(splicer, judge))
	f.usedRetries(t, 2) // the Retry Policy would stop an automatic repair here
	ranges := []domain.TimeRange{{From: 2.1, To: 2.8}}

	job, err := f.agent.StartPartial(context.Background(), "t1", repair.PartialRequest{
		Ranges: ranges, Instruction: "keep the hand still", Preserve: []domain.SpecPart{domain.PartCharacter, domain.PartProduct},
	})
	require.NoError(t, err)
	assert.Equal(t, domain.ShotRepairing, f.shotState(t))
	assert.Contains(t, f.chat.texts()[0], "2.1-2.8秒")

	require.NoError(t, f.agent.Handle(context.Background(), job))

	require.Len(t, f.regen.regenCalls, 1)
	got := f.regen.regenCalls[0]
	assert.Equal(t, domain.RepairPartialRegeneration, got.Action)
	assert.Equal(t, ranges, got.Ranges)
	assert.Equal(t, "keep the hand still", got.Direction)
	assert.Equal(t, []string{"character", "product"}, got.Preserve)
	assert.Equal(t, domain.ModelName("kling-3.0"), got.SourceModel)
	assert.Equal(t, []splicerCall{{"sh1", "t1", "new-take-1", ranges}}, splicer.calls)
	assert.Equal(t, []string{"spliced-1"}, judge.started)
	assert.Empty(t, f.chat.escalations)
	assert.Equal(t, domain.ShotPlanning, f.shotState(t))
}

func TestHandle_APartialRegenerationFailureIsReportedAndNothingIsSpliced(t *testing.T) {
	splicer := &fakeSplicer{id: "spliced-1"}
	f := newFx(t, withSplicer(splicer, &fakeTakeJudge{}))
	f.regen.regenErr = errProvider
	job, err := f.agent.StartPartial(context.Background(), "t1", repair.PartialRequest{Ranges: []domain.TimeRange{{From: 1, To: 2}}})
	require.NoError(t, err)

	err = f.agent.Handle(context.Background(), job)

	require.EqualError(t, err, generation.FailureReason(errProvider))
	assert.Empty(t, splicer.calls)
	assert.Contains(t, f.chat.texts()[len(f.chat.texts())-1], "Repair に失敗しました")
	assert.Equal(t, generation.StageFailed, f.progress.last().Stage)
	assert.Equal(t, domain.ShotPlanning, f.shotState(t))
}

// jobOf stores a finished job of the Shot and returns its ID.
func (f *fx) jobOf(t *testing.T, kind domain.JobKind, payload string, status domain.JobStatus) string {
	t.Helper()
	job, err := f.jobs.repo.Create(context.Background(), kind, json.RawMessage(payload))
	require.NoError(t, err)
	switch status {
	case domain.JobSucceeded:
		require.NoError(t, f.jobs.repo.MarkRunning(context.Background(), job.ID))
		require.NoError(t, f.jobs.repo.MarkSucceeded(context.Background(), job.ID))
	case domain.JobFailed:
		require.NoError(t, f.jobs.repo.MarkRunning(context.Background(), job.ID))
		require.NoError(t, f.jobs.repo.MarkFailed(context.Background(), job.ID, "boom"))
	}
	return job.ID
}

func TestJobChanged_TheAutomaticLoopRepairsTheBestJudgedTakeOfAFailingShot(t *testing.T) {
	f := newFx(t)
	f.addTake(t, "t2", "p1/b.mp4")
	score := func(v float64) map[domain.EvaluationAxis]float64 {
		s := map[domain.EvaluationAxis]float64{}
		for _, a := range domain.TemplateProductCM.Axes() {
			s[a] = v
		}
		return s
	}
	f.judge(t, "t1", domain.JudgeResult{Result: domain.JudgeFail, Scores: score(30)})
	f.judge(t, "t2", domain.JudgeResult{Result: domain.JudgeFail, Scores: score(60)})
	jobID := f.jobOf(t, domain.JobJudge, `{"take_id":"t2","shot_id":"sh1"}`, domain.JobSucceeded)

	f.agent.JobChanged(context.Background(), jobID)

	enqueued := f.jobs.enqueued()
	require.Len(t, enqueued, 1)
	assert.Equal(t, domain.JobRepair, enqueued[0].Kind)
	var p struct {
		TakeID string `json:"take_id"`
		Auto   bool   `json:"auto"`
	}
	require.NoError(t, json.Unmarshal(enqueued[0].Payload, &p))
	assert.Equal(t, "t2", p.TakeID, "the better take is the one to repair from")
	assert.True(t, p.Auto)
	assert.Equal(t, domain.ShotRepairing, f.shotState(t))
}

func TestJobChanged_AGenerateJobStartsTheLoopToo(t *testing.T) {
	f := newFx(t)
	jobID := f.jobOf(t, domain.JobGenerate, `{"action":"generate","shot_id":"sh1"}`, domain.JobSucceeded)

	f.agent.JobChanged(context.Background(), jobID)

	assert.Len(t, f.jobs.enqueued(), 1)
}

func TestJobChanged_DoesNothingWhenThereIsNothingToRepairYet(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, f *fx) string // returns the job ID to report
	}{
		{"unknown job", func(*testing.T, *fx) string { return "ghost" }},
		{"job failed", func(t *testing.T, f *fx) string {
			return f.jobOf(t, domain.JobJudge, `{"take_id":"t1","shot_id":"sh1"}`, domain.JobFailed)
		}},
		{"job still queued", func(t *testing.T, f *fx) string {
			return f.jobOf(t, domain.JobJudge, `{"take_id":"t1","shot_id":"sh1"}`, domain.JobQueued)
		}},
		{"a repair job ended", func(t *testing.T, f *fx) string {
			return f.jobOf(t, domain.JobRepair, `{"take_id":"t1","shot_id":"sh1"}`, domain.JobSucceeded)
		}},
		{"a reference pack made no take", func(t *testing.T, f *fx) string {
			return f.jobOf(t, domain.JobGenerate, `{"action":"reference_pack","shot_id":"sh1"}`, domain.JobSucceeded)
		}},
		{"payload is not json", func(t *testing.T, f *fx) string {
			return f.jobOf(t, domain.JobJudge, `"text"`, domain.JobSucceeded)
		}},
		{"payload names no shot", func(t *testing.T, f *fx) string {
			return f.jobOf(t, domain.JobJudge, `{"take_id":"t1"}`, domain.JobSucceeded)
		}},
		{"director mode never repairs by itself", func(t *testing.T, f *fx) string {
			f.setProject(t, `mode = 'director'`)
			return f.jobOf(t, domain.JobJudge, `{"take_id":"t1","shot_id":"sh1"}`, domain.JobSucceeded)
		}},
		{"shot is still generating", func(t *testing.T, f *fx) string {
			require.NoError(t, f.gens.SetShotState(context.Background(), "sh1", domain.ShotGenerating))
			return f.jobOf(t, domain.JobJudge, `{"take_id":"t1","shot_id":"sh1"}`, domain.JobSucceeded)
		}},
		{"shot is being repaired", func(t *testing.T, f *fx) string {
			require.NoError(t, f.gens.SetShotState(context.Background(), "sh1", domain.ShotRepairing))
			return f.jobOf(t, domain.JobJudge, `{"take_id":"t1","shot_id":"sh1"}`, domain.JobSucceeded)
		}},
		{"another take is not judged yet", func(t *testing.T, f *fx) string {
			f.addTake(t, "t2", "p1/b.mp4")
			return f.jobOf(t, domain.JobJudge, `{"take_id":"t1","shot_id":"sh1"}`, domain.JobSucceeded)
		}},
		{"another take is being judged", func(t *testing.T, f *fx) string {
			f.addTake(t, "t2", "p1/b.mp4")
			f.judge(t, "t2", failing(domain.JudgeIssue{Type: "flicker", Severity: domain.SeverityLow}))
			f.jobOf(t, domain.JobJudge, `{"take_id":"t2","shot_id":"sh1"}`, domain.JobQueued)
			return f.jobOf(t, domain.JobJudge, `{"take_id":"t1","shot_id":"sh1"}`, domain.JobSucceeded)
		}},
		{"a take is accepted", func(t *testing.T, f *fx) string {
			f.addTake(t, "t2", "p1/b.mp4")
			f.judge(t, "t2", domain.JudgeResult{Result: domain.JudgePass})
			return f.jobOf(t, domain.JobJudge, `{"take_id":"t2","shot_id":"sh1"}`, domain.JobSucceeded)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFx(t)
			id := tt.setup(t, f)
			before := f.shotState(t)

			f.agent.JobChanged(context.Background(), id)

			assert.Empty(t, f.jobs.enqueued())
			assert.Equal(t, before, f.shotState(t))
		})
	}
}

func TestContinue_ALostJobIsLoggedAndTheShotIsReleased(t *testing.T) {
	f := newFx(t)
	f.jobs.err = errors.New("queue is closed")

	f.agent.Continue(context.Background(), "sh1")

	assert.Contains(t, f.logs.String(), "repair loop: start failed")
	assert.Equal(t, domain.ShotPlanning, f.shotState(t))
}

func TestContinue_UnknownShotOrProjectIsIgnored(t *testing.T) {
	f := newFx(t)

	f.agent.Continue(context.Background(), "ghost")

	assert.Empty(t, f.jobs.enqueued())
	assert.Empty(t, f.logs.String())
}

func TestFailureReason_NamesWhatTheUserCanFix(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{generation.ErrNoSpec, "Shot Spec"},
		{generation.ErrNoOtherModel, "別の動画モデル"},
		{fmt.Errorf("wrapped: %w", repository.ErrLocked), "LOCK"},
		{generation.ErrBudgetExceeded, "予算"},
	}
	for _, tt := range tests {
		assert.Contains(t, repair.FailureReason(tt.err), tt.want)
	}
}

func TestSummarize(t *testing.T) {
	tests := []struct {
		name string
		res  domain.JudgeResult
		want string
	}{
		{"failed without issues", domain.JudgeResult{Result: domain.JudgeFail}, "FAIL"},
		{"passed below the minimum scores", domain.JudgeResult{Result: domain.JudgePass}, "PASS（最低スコア未達）"},
		{"issues and a LOCK violation", failing(
			domain.JudgeIssue{Type: "flicker", Severity: domain.SeverityLow},
			domain.JudgeIssue{Type: "face", Severity: domain.SeverityHigh, LockViolation: true},
		), "FAIL — flicker[low], face[high]（LOCK 違反）"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { assert.Equal(t, tt.want, repair.Summarize(tt.res)) })
	}
}
