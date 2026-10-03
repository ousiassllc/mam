package generation_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func (f *fixture) candidates(t *testing.T, instruction string) error {
	t.Helper()
	return f.run(context.Background(), t, func() (domain.Job, error) {
		return f.orch.StartCandidates(context.Background(), f.shotID, instruction)
	})
}

func (f *fixture) statusOf(t *testing.T, takeID string) domain.TakeStatus {
	t.Helper()
	var s domain.TakeStatus
	require.NoError(t, f.db.Reader().QueryRow(`SELECT status FROM take WHERE id = ?`, takeID).Scan(&s))
	return s
}

func failing(issues ...domain.JudgeIssue) domain.JudgeResult {
	return domain.JudgeResult{Result: domain.JudgeFail, OverallConfidence: 0.8, Issues: issues}
}

func TestOrchestrator_StartCandidatesNeedsTheJudge(t *testing.T) {
	f := newFixture(t, options{})

	_, err := f.orch.StartCandidates(context.Background(), f.shotID, "")

	assert.ErrorIs(t, err, generation.ErrJudgeUnavailable)
	assert.Zero(t, f.jobs.count())
	assert.Equal(t, domain.ShotPlanning, f.shotState(t))
	assert.Contains(t, generation.FailureReason(err), "AI Video Judge")
}

func TestOrchestrator_QualityModeGeneratesEveryCandidateAndMarksTheBestTake(t *testing.T) {
	f := newFixture(t, options{withJudge: true, quality: domain.QualityMaxQuality})
	// Every Take passes; the third judgement is the only one without an Issue, so its Take scores highest.
	f.judge.verdicts = func(n int) (domain.JudgeResult, error) {
		res := domain.JudgeResult{Result: domain.JudgePass, OverallConfidence: 0.9}
		if n != 3 {
			res.Issues = []domain.JudgeIssue{{Type: "flicker", Severity: domain.SeverityLow, Dimension: domain.DimFlicker}}
		}
		return res, nil
	}

	require.NoError(t, f.candidates(t, ""))

	strategies := domain.CandidateStrategies()
	assert.Equal(t, len(strategies), f.llm.calls(llm.AgentPromptPlanner), "one Candidate spec per shooting strategy")
	assert.Equal(t, len(strategies), f.count(t, `SELECT COUNT(*) FROM take`))
	for _, s := range strategies {
		assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM take WHERE model_parameters LIKE ?`, `%"candidate_strategy":"`+string(s)+`"%`), s)
	}
	results := f.judge.results()
	require.Len(t, results, len(strategies))
	var best string
	for id, r := range results {
		if len(r.Issues) == 0 {
			best = id
		}
	}
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM take WHERE status = 'ai_pick'`))
	assert.Equal(t, domain.TakeAIPick, f.statusOf(t, best), "the highest scoring passing Take is the AI pick")

	summary := f.chat.ofKind(domain.KindText)
	require.Len(t, summary, 1)
	assert.Contains(t, summary[0].Content, "最良の Take として推奨します")
	assert.Equal(t, generation.ActionCandidates, f.progress.last().Action)
	assert.Equal(t, generation.StageDone, f.progress.last().Stage)
	assert.Equal(t, len(strategies), f.progress.last().TakesDone)
	assert.Equal(t, domain.ShotPlanning, f.shotState(t))
}

// The Take AUTO MODE selects is chosen among the Takes the Retry Policy accepts; the AI Pick of a
// candidate run must be that Take, not a pass that scores best by its Issues but under the minimum.
func TestOrchestrator_QualityModeRecommendsATakeTheRetryPolicyAccepts(t *testing.T) {
	f := newFixture(t, options{withJudge: true, quality: domain.QualityMaxQuality})
	template := domain.TemplateProductCM
	f.judge.verdicts = func(n int) (domain.JudgeResult, error) {
		res := domain.JudgeResult{Result: domain.JudgePass, OverallConfidence: 0.9, Scores: map[domain.EvaluationAxis]float64{}}
		score := 100.0
		if n == 1 { // no Issue (the best Take score) but a score under every minimum
			score = 1
		} else {
			res.Issues = []domain.JudgeIssue{{Type: "flicker", Severity: domain.SeverityLow, Dimension: domain.DimFlicker}}
		}
		for _, a := range template.Axes() {
			res.Scores[a] = score
		}
		return res, nil
	}

	require.NoError(t, f.candidates(t, ""))

	var picked string
	require.NoError(t, f.db.Reader().QueryRow(`SELECT id FROM take WHERE status = 'ai_pick'`).Scan(&picked))
	res := f.judge.results()[picked]
	assert.Equal(t, 100.0, res.Scores[template.Axes()[0]], "the AI Pick is a Take that reaches the Minimum acceptance score")
}

func TestOrchestrator_BalancedModeAddsACandidateOnlyWhileTheJudgeFailsTheTake(t *testing.T) {
	cases := []struct {
		name      string
		verdicts  func(n int) (domain.JudgeResult, error)
		wantTakes int
		wantStop  string
	}{
		{"first passes", func(int) (domain.JudgeResult, error) {
			return domain.JudgeResult{Result: domain.JudgePass, OverallConfidence: 0.9}, nil
		}, 1, ""},
		{"second passes", func(n int) (domain.JudgeResult, error) {
			if n == 1 {
				return failing(), nil
			}
			return domain.JudgeResult{Result: domain.JudgePass, OverallConfidence: 0.9}, nil
		}, 2, ""},
		{"every candidate fails: the retry budget ends the run", func(int) (domain.JudgeResult, error) { return failing(), nil }, 3, ""},
		{"judge cannot evaluate: nothing more is spent", func(int) (domain.JudgeResult, error) {
			return domain.JudgeResult{}, errors.New("judge is down")
		}, 1, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, options{withJudge: true})
			f.judge.verdicts = c.verdicts

			require.NoError(t, f.candidates(t, ""))

			assert.Equal(t, c.wantTakes, f.count(t, `SELECT COUNT(*) FROM take`))
			assert.Equal(t, c.wantTakes, f.llm.calls(llm.AgentPromptPlanner))
			assert.Equal(t, c.wantTakes, f.progress.last().TakesDone)
		})
	}
}

func TestOrchestrator_BalancedModeMakesNoMoreTakesThanTheRetryPolicyAllows(t *testing.T) {
	f := newFixture(t, options{withJudge: true, limits: &domain.PolicyLimits{MaxRetries: new(0)}})
	f.judge.verdicts = func(int) (domain.JudgeResult, error) { return failing(), nil }

	require.NoError(t, f.candidates(t, ""))

	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM take`), "no retry is allowed")
	summary := f.chat.ofKind(domain.KindText)
	require.Len(t, summary, 1)
	assert.Contains(t, summary[0].Content, "基準を満たす Take はありませんでした", "the least bad Take is still recommended")
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM take WHERE status = 'ai_pick'`))
}

// The Max retries per Shot is one limit over the retaking Repairs and the Takes of Prompt Candidate
// Generation (spec/spec.md §8): a Shot that used its retries on Repair gets the first Take of a
// candidate run and no more.
func TestOrchestrator_BalancedModeCountsRepairsAlreadyDoneAgainstTheRetryPolicy(t *testing.T) {
	f := newFixture(t, options{withJudge: true, limits: &domain.PolicyLimits{MaxRetries: new(2)}})
	ctx := context.Background()
	require.NoError(t, f.candidates(t, "")) // the first run: one passing Take
	var takeID string
	require.NoError(t, f.db.Reader().QueryRow(`SELECT id FROM take`).Scan(&takeID))
	judged, err := repository.NewJudgeRepository(f.db).Save(ctx, domain.JudgeResult{TakeID: takeID, Result: domain.JudgeFail})
	require.NoError(t, err)
	repairs := repository.NewRepairRepository(f.db)
	for range 2 { // two retaking Repairs that made a Take
		rec, err := repairs.CreateAction(ctx, judged.ID, domain.RepairFullRetake, domain.RepairInstructions{})
		require.NoError(t, err)
		require.NoError(t, repairs.SetResultingTake(ctx, rec.ID, takeID))
	}
	before := f.count(t, `SELECT COUNT(*) FROM take`)
	f.judge.verdicts = func(int) (domain.JudgeResult, error) { return failing(), nil }

	require.NoError(t, f.candidates(t, ""))

	assert.Equal(t, before+1, f.count(t, `SELECT COUNT(*) FROM take`), "only the first Take of the run: the Repairs used the retries")
	summary := f.chat.ofKind(domain.KindText)
	require.Len(t, summary, 2)
	assert.Contains(t, summary[1].Content, "リトライ上限")
}

func TestOrchestrator_CandidateRunsShareOneRetryLimit(t *testing.T) {
	for _, quality := range []domain.QualityPolicy{domain.QualityBalanced, domain.QualityFast} {
		t.Run(string(quality), func(t *testing.T) {
			f := newFixture(t, options{withJudge: true, quality: quality, limits: &domain.PolicyLimits{MaxRetries: new(2)}})
			f.judge.verdicts = func(int) (domain.JudgeResult, error) {
				return failing(domain.JudgeIssue{Type: "face_identity_drift", Severity: domain.SeverityHigh}), nil
			}

			require.NoError(t, f.candidates(t, ""))
			require.Equal(t, 3, f.count(t, `SELECT COUNT(*) FROM take`), "the first run spends both retries")
			require.NoError(t, f.candidates(t, ""))

			assert.Equal(t, 4, f.count(t, `SELECT COUNT(*) FROM take`), "the second run makes its first Take and counts the earlier ones")
		})
	}
}

func TestOrchestrator_BalancedModeStopsWhenTheBudgetIsSpent(t *testing.T) {
	f := newFixture(t, options{withJudge: true})
	cfg := modeladapter.DefaultConfig()
	perTake := cfg.VideoCost(cfg.Policies[domain.QualityBalanced].Primary, 4)
	require.NoError(t, f.policies.SetBudget(context.Background(), f.projectID, new(perTake*1.2), nil))
	f.judge.verdicts = func(int) (domain.JudgeResult, error) { return failing(), nil }

	require.NoError(t, f.candidates(t, ""))

	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM take`), "the budget pays for one Take only")
	summary := f.chat.ofKind(domain.KindText)
	require.Len(t, summary, 1)
	assert.Contains(t, summary[0].Content, "予算上限")
}

func TestOrchestrator_EconomyModeRetakesOnlyAfterASevereProblem(t *testing.T) {
	severe := failing(domain.JudgeIssue{Type: "face_identity_drift", Severity: domain.SeverityHigh})
	minor := failing(domain.JudgeIssue{Type: "motion_jitter", Severity: domain.SeverityLow})
	cases := []struct {
		name       string
		verdicts   func(n int) (domain.JudgeResult, error)
		wantTakes  int
		wantNotice string
	}{
		{"minor problem is kept", func(int) (domain.JudgeResult, error) { return minor, nil }, 1, "基準を満たす Take はありませんでした"},
		{"severe problem is retaken once", func(n int) (domain.JudgeResult, error) {
			if n == 1 {
				return severe, nil
			}
			return minor, nil
		}, 2, "基準を満たす Take はありませんでした"},
		{"retry policy caps the retakes", func(int) (domain.JudgeResult, error) { return severe, nil }, 2, "リトライ上限"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, options{withJudge: true, quality: domain.QualityFast})
			f.judge.verdicts = c.verdicts

			require.NoError(t, f.candidates(t, ""))

			assert.Equal(t, c.wantTakes, f.count(t, `SELECT COUNT(*) FROM take`))
			assert.Equal(t, 1, f.llm.calls(llm.AgentPromptPlanner), "an Economy retake reuses the Candidate spec")
			assert.Equal(t, c.wantTakes, f.count(t, `SELECT COUNT(*) FROM take WHERE model_parameters LIKE '%"candidate_strategy":"stability_first"%'`), "Economy mode only shoots Stability First")
			summary := f.chat.ofKind(domain.KindText)
			require.Len(t, summary, 1)
			assert.Contains(t, summary[0].Content, c.wantNotice)
		})
	}
}

func TestOrchestrator_CandidatesSurvivePartialFailureAndReportTotalFailure(t *testing.T) {
	t.Run("one Candidate fails", func(t *testing.T) {
		f := newFixture(t, options{withJudge: true, quality: domain.QualityMaxQuality, videoModels: []domain.ModelName{domain.ModelRunwayGen45}})
		f.video.failOn = func(n int) error {
			if n == 2 {
				return rejected()
			}
			return nil
		}

		require.NoError(t, f.candidates(t, ""))

		assert.Equal(t, 2, f.count(t, `SELECT COUNT(*) FROM take`))
		assert.Len(t, f.judge.results(), 2, "the Takes that exist are judged")
		assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM take WHERE status = 'ai_pick'`))
		assert.Equal(t, 2, f.progress.last().TakesDone)
	})

	t.Run("every Candidate fails", func(t *testing.T) {
		f := newFixture(t, options{withJudge: true})
		f.video.failOn = func(int) error { return rejected() }

		err := f.candidates(t, "")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "拒否")
		assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM take`))
		assert.Empty(t, f.judge.results())
		assert.Equal(t, domain.ShotPlanning, f.shotState(t))
		assert.Equal(t, generation.StageFailed, f.progress.last().Stage)
	})

	t.Run("planner fails", func(t *testing.T) {
		f := newFixture(t, options{withJudge: true})
		f.llm.errs = map[llm.Agent]error{llm.AgentPromptPlanner: llm.ErrNoAPIKey}

		err := f.candidates(t, "")

		require.Error(t, err)
		assert.Zero(t, f.video.callCount())
	})
}

func TestOrchestrator_CandidatesWithoutAnyJudgement(t *testing.T) {
	f := newFixture(t, options{withJudge: true, quality: domain.QualityMaxQuality})
	f.judge.verdicts = func(int) (domain.JudgeResult, error) { return domain.JudgeResult{}, errors.New("judge is down") }

	require.NoError(t, f.candidates(t, ""))

	assert.Equal(t, 3, f.count(t, `SELECT COUNT(*) FROM take`), "unjudged Takes are kept")
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM take WHERE status = 'ai_pick'`), "there is no Best Take without a verdict")
	summary := f.chat.ofKind(domain.KindText)
	require.Len(t, summary, 1)
	assert.Contains(t, summary[0].Content, "最良の Take は選べませんでした")
}

func TestOrchestrator_StartCandidatesRefusesALockedPrompt(t *testing.T) {
	f := newFixture(t, options{withJudge: true})
	require.NoError(t, f.generate(t, "")) // the Shot has a spec now
	require.NoError(t, repository.NewLockRepository(f.db).SetShotLock(context.Background(), f.shotID, domain.ParamPrompt, domain.LockLocked, nil))

	_, err := f.orch.StartCandidates(context.Background(), f.shotID, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt")
	assert.Equal(t, domain.ShotPlanning, f.shotState(t))
}

// Every generation the system starts for the user (the Shot card, Continue, Branch, the editor's
// reshoot, the director) goes through StartByPolicy, which makes Prompt Candidates whenever the Quality
// Policy can (#437); the single generation stays the explicit Start.
func TestOrchestrator_StartByPolicyGeneratesCandidatesAndOnlyFallsBackToASingleTakeWhenItMust(t *testing.T) {
	byPolicy := func(f *fixture, t *testing.T, preserve []domain.SpecPart) error {
		return f.run(context.Background(), t, func() (domain.Job, error) {
			return f.orch.StartByPolicy(context.Background(), f.shotID, "", preserve)
		})
	}

	t.Run("MAX QUALITY makes a Candidate per strategy", func(t *testing.T) {
		f := newFixture(t, options{withJudge: true, quality: domain.QualityMaxQuality})
		uses, err := f.orch.UsesCandidates(context.Background(), f.ref(t).Shot)
		require.NoError(t, err)
		assert.True(t, uses)

		require.NoError(t, byPolicy(f, t, nil))

		assert.Equal(t, len(domain.CandidateStrategies()), f.count(t, `SELECT COUNT(*) FROM take WHERE model_parameters LIKE '%candidate_strategy%'`))
		assert.Equal(t, generation.ActionCandidates, f.progress.last().Action)
	})
	t.Run("without the Judge nothing could pick the Best Take: a single generation", func(t *testing.T) {
		f := newFixture(t, options{quality: domain.QualityMaxQuality})
		uses, err := f.orch.UsesCandidates(context.Background(), f.ref(t).Shot)
		require.NoError(t, err)
		assert.False(t, uses)

		require.NoError(t, byPolicy(f, t, nil))

		assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM take WHERE model_parameters LIKE '%candidate_strategy%'`))
		assert.Positive(t, f.count(t, `SELECT COUNT(*) FROM take`))
	})
	t.Run("a LOCKed Prompt cannot be rewritten: a single generation from the spec the Shot has", func(t *testing.T) {
		f := newFixture(t, options{withJudge: true, quality: domain.QualityMaxQuality})
		require.NoError(t, f.generate(t, ""))
		require.NoError(t, repository.NewLockRepository(f.db).SetShotLock(context.Background(), f.shotID, domain.ParamPrompt, domain.LockLocked, nil))
		uses, err := f.orch.UsesCandidates(context.Background(), f.ref(t).Shot)
		require.NoError(t, err)
		assert.False(t, uses)
		takes := f.count(t, `SELECT COUNT(*) FROM take`)

		require.NoError(t, byPolicy(f, t, nil))

		assert.Greater(t, f.count(t, `SELECT COUNT(*) FROM take`), takes)
		assert.Equal(t, generation.ActionGenerate, f.progress.last().Action)
	})
	t.Run("the explicit generate is still the single generation", func(t *testing.T) {
		f := newFixture(t, options{withJudge: true, quality: domain.QualityMaxQuality})
		require.NoError(t, f.generate(t, ""))
		assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM take WHERE model_parameters LIKE '%candidate_strategy%'`))
	})
	t.Run("the parts the director preserves reach the Prompt Planner of every Candidate", func(t *testing.T) {
		f := newFixture(t, options{withJudge: true, quality: domain.QualityMaxQuality})
		require.NoError(t, f.generate(t, "")) // the Shot has a spec now
		planner := f.llm.calls(llm.AgentPromptPlanner)

		require.NoError(t, byPolicy(f, t, []domain.SpecPart{domain.PartCharacter}))

		assert.Equal(t, len(domain.CandidateStrategies()), f.llm.calls(llm.AgentPromptPlanner)-planner)
		assert.Contains(t, f.llm.lastUser(llm.AgentPromptPlanner), string(domain.PartCharacter))
	})
}
