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
