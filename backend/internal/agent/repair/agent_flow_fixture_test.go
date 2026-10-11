package repair_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/jev"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/repair"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// fakeRegen is the Regenerator: it plans with a per-action cost or error and "generates" by inserting
// a Take row, so that the repair record can point at a real Take.
type fakeRegen struct {
	db *repository.DB

	mu          sync.Mutex
	strategyErr map[domain.RepairAction]error
	cost        map[domain.RepairAction]float64
	regenErr    error
	// beforeRegenerate runs at the start of Regenerate (a test cancels the context there).
	beforeRegenerate func()
	strategyCalls    []domain.RepairAction
	prepareCalls     int // PreparePartial calls: each one is a Prompt Planner call in production
	regenCalls       []generation.Regeneration
	made             []string
}

func (f *fakeRegen) RegenerationStrategy(_ context.Context, _ repository.ShotRef, r generation.Regeneration) (generation.Strategy, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.strategyCalls = append(f.strategyCalls, r.Action)
	if err := f.strategyErr[r.Action]; err != nil {
		return generation.Strategy{}, err
	}
	return generation.Strategy{CostPerTake: f.cost[r.Action]}, nil
}

// PreparePartial stands for the Prompt Planner call of a Partial Regeneration; it prepares nothing.
func (f *fakeRegen) PreparePartial(context.Context, repository.ShotRef, generation.Regeneration) (domain.ShotSpec, []domain.NoteRef, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prepareCalls++
	return domain.ShotSpec{}, nil, false, nil
}

func (f *fakeRegen) Regenerate(ctx context.Context, _ repository.ShotRef, r generation.Regeneration) (string, error) {
	if f.beforeRegenerate != nil {
		f.beforeRegenerate()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.regenCalls = append(f.regenCalls, r)
	if f.regenErr != nil {
		return "", f.regenErr
	}
	id := fmt.Sprintf("new-take-%d", len(f.made)+1)
	err := f.db.Write(context.WithoutCancel(ctx), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO take (id, prompt_version_id, model_name, file_path) VALUES (?, 'pv1', 'kling-3.0', 'p1/new.mp4')`, id)
		return err
	})
	if err != nil {
		return "", err
	}
	f.made = append(f.made, id)
	return id, nil
}

// fakeChat records the chat thread.
type fakeChat struct {
	mu            sync.Mutex
	announced     []domain.ChatMessage
	escalations   []escalation
	announceErr   error
	escalationErr error
}

type escalation struct {
	ProjectID, Text, ShotID string
	Options                 []string
}

func (c *fakeChat) Announce(_ context.Context, _ string, m domain.ChatMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.announced = append(c.announced, m)
	return c.announceErr
}

func (c *fakeChat) RaiseEscalation(_ context.Context, projectID, text string, options []string, shotID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.escalations = append(c.escalations, escalation{projectID, text, shotID, options})
	return c.escalationErr
}

func (c *fakeChat) texts() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.announced))
	for i, m := range c.announced {
		out[i] = m.Content
	}
	return out
}

// fakeJobs stores the jobs it is given in the job table, as the real queue does.
type fakeJobs struct {
	repo *repository.JobRepository
	err  error

	mu   sync.Mutex
	jobs []domain.Job
}

func (j *fakeJobs) Enqueue(ctx context.Context, kind domain.JobKind, payload any) (domain.Job, error) {
	if j.err != nil {
		return domain.Job{}, j.err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return domain.Job{}, err
	}
	job, err := j.repo.Create(ctx, kind, raw)
	if err != nil {
		return domain.Job{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.jobs = append(j.jobs, job)
	return job, nil
}

func (j *fakeJobs) enqueued() []domain.Job {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]domain.Job(nil), j.jobs...)
}

type splicerCall struct {
	ShotID, BaseTakeID, RegeneratedTakeID string
	Ranges                                []domain.TimeRange
}

type fakeSplicer struct {
	id    string
	err   error
	calls []splicerCall
}

func (s *fakeSplicer) SpliceRegenerated(_ context.Context, shotID, baseTakeID, regeneratedTakeID string, ranges []domain.TimeRange) (string, error) {
	s.calls = append(s.calls, splicerCall{shotID, baseTakeID, regeneratedTakeID, ranges})
	return s.id, s.err
}

type fakeTakeJudge struct {
	err     error
	started []string
}

func (j *fakeTakeJudge) Start(_ context.Context, takeID string) (domain.Job, error) {
	j.started = append(j.started, takeID)
	return domain.Job{ID: "judge-job"}, j.err
}

// fakeLLM is the Reasoning LLM.
type fakeLLM struct {
	text  string
	err   error
	calls int
}

func (l *fakeLLM) Complete(context.Context, llm.Agent, llm.Request) (llm.Response, error) {
	l.calls++
	return llm.Response{Text: l.text}, l.err
}

func answer(action domain.RepairAction, instructions ...string) string {
	raw, _ := json.Marshal(map[string]any{"action": string(action), "reason": "because " + string(action), "instructions": instructions})
	return string(raw)
}

type fakeShadow struct {
	choice jev.Choice
	err    error
	calls  int
}

func (s *fakeShadow) Choose(context.Context, jev.Input) (jev.Choice, error) {
	s.calls++
	return s.choice, s.err
}

func (s *fakeShadow) Model() string { return "jev-test" }

// progressLog collects Notify calls.
type progressLog struct {
	mu   sync.Mutex
	list []generation.Progress
}

func (p *progressLog) add(g generation.Progress) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.list = append(p.list, g)
}

func (p *progressLog) stages() []generation.Stage {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]generation.Stage, len(p.list))
	for i, g := range p.list {
		out[i] = g.Stage
	}
	return out
}

func (p *progressLog) last() generation.Progress {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.list[len(p.list)-1]
}

// fx is an Agent over a real database (project p1 / shot sh1 / judged failing take t1) and fakes.
type fx struct {
	db       *repository.DB
	agent    *repair.Agent
	regen    *fakeRegen
	chat     *fakeChat
	jobs     *fakeJobs
	llm      *fakeLLM
	progress *progressLog
	logs     *bytes.Buffer
	judges   *repository.JudgeRepository
	repairs  *repository.RepairRepository
	gens     *repository.GenerationRepository
	// judged is the latest Judge result of take t1.
	judged domain.JudgeResult
}

type fxOption func(*fx, *repair.Deps)

func withSplicer(s *fakeSplicer, j *fakeTakeJudge) fxOption {
	return func(_ *fx, d *repair.Deps) {
		d.Splicer = s
		if j != nil {
			d.Judge = j
		}
	}
}

func withShadow(s *fakeShadow) fxOption { return func(_ *fx, d *repair.Deps) { d.Shadow = s } }

func withoutLLM() fxOption { return func(_ *fx, d *repair.Deps) { d.LLM = nil } }

// newFx opens a database with project p1 (AUTO mode, balanced policy: 2 retries), one Shot and one Take
// t1 that the Judge failed with a camera_speed issue.
func newFx(t *testing.T, opts ...fxOption) *fx {
	t.Helper()
	db, err := repository.Open(context.Background(), seededCopy(t), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	f := &fx{
		db:       db,
		regen:    &fakeRegen{db: db},
		chat:     &fakeChat{},
		jobs:     &fakeJobs{repo: repository.NewJobRepository(db)},
		llm:      &fakeLLM{text: answer(domain.RepairPrompt)},
		progress: &progressLog{},
		logs:     &bytes.Buffer{},
		judges:   repository.NewJudgeRepository(db),
		repairs:  repository.NewRepairRepository(db),
		gens:     repository.NewGenerationRepository(db),
	}
	deps := repair.Deps{
		Generations: f.gens, Judges: f.judges, Repairs: f.repairs, JobRepo: f.jobs.repo,
		Policies: repository.NewPolicyRepository(db), Projects: repository.NewProjectRepository(db), Takes: repository.NewTakeRepository(db),
		Regenerator: f.regen, Chat: f.chat, Jobs: f.jobs, LLM: f.llm, Notify: f.progress.add,
		Logger: slog.New(slog.NewTextHandler(f.logs, nil)),
	}
	for _, o := range opts {
		o(f, &deps)
	}
	f.agent = repair.New(deps)
	f.judged = f.judge(t, "t1", failing(domain.JudgeIssue{Type: "camera_speed", Severity: domain.SeverityMedium}))
	return f
}

func execAll(t *testing.T, db *repository.DB, queries ...string) {
	t.Helper()
	require.NoError(t, db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		for _, q := range queries {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return fmt.Errorf("%s: %w", q, err)
			}
		}
		return nil
	}))
}

func failing(issues ...domain.JudgeIssue) domain.JudgeResult {
	return domain.JudgeResult{Result: domain.JudgeFail, Issues: issues}
}

// judge stores a Judge result for the Take.
func (f *fx) judge(t *testing.T, takeID string, res domain.JudgeResult) domain.JudgeResult {
	t.Helper()
	res.TakeID = takeID
	saved, err := f.judges.Save(context.Background(), res)
	require.NoError(t, err)
	return saved
}

func (f *fx) addTake(t *testing.T, id, file string) {
	t.Helper()
	execAll(t, f.db, fmt.Sprintf(`INSERT INTO take (id, prompt_version_id, model_name, file_path) VALUES ('%s', 'pv1', 'kling-3.0', '%s')`, id, file))
}

// usedRetries records n finished retries of the Shot (repairs that made a Take).
func (f *fx) usedRetries(t *testing.T, n int) {
	t.Helper()
	ctx := context.Background()
	for i := range n {
		takeID := fmt.Sprintf("old-%d", i)
		f.addTake(t, takeID, "p1/old.mp4")
		rec, err := f.repairs.CreateAction(ctx, f.judged.ID, domain.RepairFullRetake, domain.RepairInstructions{SourceTakeID: "t1", DecidedBy: domain.DecidedByRule})
		require.NoError(t, err)
		require.NoError(t, f.repairs.SetResultingTake(ctx, rec.ID, takeID))
	}
}

// usedCandidates records that Prompt Candidate Generation made n Takes of the Shot: take t1 and n-1
// more, all carrying a candidate strategy as the candidate run writes it.
func (f *fx) usedCandidates(t *testing.T, n int) {
	t.Helper()
	for i := 1; i < n; i++ {
		f.addTake(t, fmt.Sprintf("cand-%d", i+1), "p1/cand.mp4")
	}
	execAll(t, f.db, `UPDATE take SET model_parameters = '{"candidate_strategy":"stability_first"}' WHERE id = 't1' OR id LIKE 'cand-%'`)
}

func (f *fx) setProject(t *testing.T, set string) {
	t.Helper()
	execAll(t, f.db, `UPDATE project SET `+set+` WHERE id = 'p1'`)
}

func (f *fx) shotState(t *testing.T) domain.ShotState {
	t.Helper()
	ref, err := f.gens.GetShot(context.Background(), "sh1")
	require.NoError(t, err)
	return ref.Shot.State
}

func (f *fx) history(t *testing.T) []domain.RepairRecord {
	t.Helper()
	h, err := f.repairs.ShotActions(context.Background(), "sh1")
	require.NoError(t, err)
	return h
}

// run queues a repair of take t1 and runs its job.
func (f *fx) run(t *testing.T, auto bool) error {
	t.Helper()
	job, err := f.agent.Start(context.Background(), "t1", auto)
	require.NoError(t, err)
	return f.agent.Handle(context.Background(), job)
}

var errProvider = errors.New("video provider exploded")

var (
	templateOnce sync.Once
	templatePath string
	templateErr  error
)

// seededCopy returns a private copy of a migrated database holding project p1 / scene sc1 / shot sh1 /
// prompt version pv1 / take t1. Migrating once and copying keeps the many tests here fast.
func seededCopy(t *testing.T) string {
	t.Helper()
	templateOnce.Do(func() {
		dir, err := os.MkdirTemp("", "repair-template-")
		if err != nil {
			templateErr = err
			return
		}
		templateDir = dir
		templatePath = filepath.Join(dir, "template.db")
		db, err := repository.Open(context.Background(), templatePath, nil)
		if err != nil {
			templateErr = err
			return
		}
		templateErr = db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
			for _, q := range []string{
				`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p1', 'auto', 'full_auto', 'product_cm', 'balanced')`,
				`INSERT INTO scene (id, project_id, title, order_index) VALUES ('sc1', 'p1', 'S', 0)`,
				`INSERT INTO shot (id, scene_id, objective, duration_seconds, order_index) VALUES ('sh1', 'sc1', 'run', 4, 0)`,
				`INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot) VALUES ('pv1', 'sh1', 1, '{"shot_objective":"run"}')`,
				`INSERT INTO take (id, prompt_version_id, model_name, file_path) VALUES ('t1', 'pv1', 'kling-3.0', 'p1/a.mp4')`,
			} {
				if _, err := tx.ExecContext(ctx, q); err != nil {
					return fmt.Errorf("%s: %w", q, err)
				}
			}
			return nil
		})
		if cerr := db.Close(); templateErr == nil {
			templateErr = cerr
		}
	})
	require.NoError(t, templateErr)
	raw, err := os.ReadFile(templatePath)
	require.NoError(t, err)
	dst := filepath.Join(t.TempDir(), "mam.db")
	require.NoError(t, os.WriteFile(dst, raw, 0o600))
	return dst
}

var templateDir string

func TestMain(m *testing.M) {
	code := m.Run()
	if templateDir != "" {
		_ = os.RemoveAll(templateDir)
	}
	os.Exit(code)
}
