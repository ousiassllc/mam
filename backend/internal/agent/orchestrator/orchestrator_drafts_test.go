package orchestrator_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/orchestrator"
	"github.com/ousiassllc/mam/backend/internal/characterdraft"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/planadvice"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// fakeDrafts stands for characterdraft.Maker: it records the Projects it is asked about and answers with res or err.
type fakeDrafts struct {
	mu       sync.Mutex
	projects []string
	res      characterdraft.Result
	err      error
}

func (d *fakeDrafts) Make(_ context.Context, projectID string) (characterdraft.Result, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.projects = append(d.projects, projectID)
	return d.res, d.err
}

func (d *fakeDrafts) asked() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.projects...)
}

func (f *fixture) withDrafts(d orchestrator.Drafts) {
	f.orch = orchestrator.New(orchestrator.Deps{
		Projects: f.projects, Plans: f.plans, ChatRepo: f.chatRepo, Chat: f.chat, Jobs: f.jobs, LLM: f.llm, Notes: f.notes, Drafts: d,
	})
}

// A plan that stops for want of an LLM API key drafts the Characters of the attached images, and the failure says so: the reason of the job
// (which the advice of planadvice reads) and the notice in the chat.
func TestOrchestrator_NoKeyFailureDraftsTheCharactersAndTellsIt(t *testing.T) {
	f := newFixture(t)
	drafts := &fakeDrafts{res: characterdraft.Result{Created: 2, Drafts: 2}}
	f.withDrafts(drafts)
	id := f.project(t, seed{brief: completeBrief()})
	f.llm.errs[llm.AgentProducer] = llm.ErrNoAPIKey

	err := f.handle(context.Background(), id)

	require.Error(t, err)
	assert.Equal(t, []string{id}, drafts.asked())
	assert.Contains(t, err.Error(), planadvice.DraftNote(2), "the reason of the job tells the drafts")
	assert.NotEmpty(t, planadvice.For(domain.Job{Kind: domain.JobPlan, Status: domain.JobFailed, Error: err.Error()}, true))
	assert.Contains(t, planadvice.For(domain.Job{Kind: domain.JobPlan, Status: domain.JobFailed, Error: err.Error()}, true), "update_character", "so the advice is of the drafts")
	f.chat.mu.Lock()
	defer f.chat.mu.Unlock()
	require.NotEmpty(t, f.chat.announced)
	assert.Contains(t, f.chat.announced[len(f.chat.announced)-1].Content, planadvice.DraftNote(2))
}

// The drafts are made for want of an API key only; a Project without any (no attached image, or Characters already) says nothing of them;
// a failure to make them never hides the failure of the plan.
func TestOrchestrator_DraftsAreMadeOnlyForWantOfAKeyAndNeverHideTheFailure(t *testing.T) {
	tests := []struct {
		name      string
		failure   error
		drafts    *fakeDrafts
		wantAsked bool
	}{
		{"another failure", llm.ErrTimeout, &fakeDrafts{res: characterdraft.Result{Created: 1, Drafts: 1}}, false},
		{"no draft to tell", llm.ErrNoAPIKey, &fakeDrafts{}, true},
		{"making them fails", llm.ErrNoAPIKey, &fakeDrafts{err: errors.New("disk full")}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.withDrafts(tt.drafts)
			id := f.project(t, seed{brief: completeBrief()})
			f.llm.errs[llm.AgentProducer] = tt.failure

			err := f.handle(context.Background(), id)

			require.Error(t, err, "the plan is failed in every case")
			assert.Equal(t, tt.wantAsked, len(tt.drafts.asked()) > 0)
			assert.NotContains(t, err.Error(), "登場人物の下書き")
		})
	}
}

// With the real Maker: the plan stopped for want of a key drafts a Character for the image attached to the Brief (face reference, Draft),
// and the plan run again by the next message (still without a key) tells the same drafts and adds none.
func TestOrchestrator_NoKeyFailureDraftsTheAttachedImageOnceWhateverTheRuns(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	world := repository.NewWorldRepository(f.db)
	f.withDrafts(characterdraft.New(f.projects, repository.NewAssetRepository(f.db), world, nil))
	id := domain.NewID()
	img := domain.Asset{ID: domain.NewID(), ProjectID: id, Type: domain.AssetImage, FilePath: id + "/a.png", MimeType: "image/png", SizeBytes: 10}
	brief := completeBrief()
	brief.AttachAssets([]domain.Asset{img})
	require.NoError(t, f.projects.Create(ctx, repository.NewProject{
		Project: domain.Project{ID: id, State: domain.StateDraft, Mode: domain.ModeAuto, ConfirmationLevel: domain.ConfirmFullAuto,
			ProductionTemplate: domain.TemplateProductCM, QualityPolicy: domain.QualityBalanced},
		Brief: brief, Assets: []domain.Asset{img},
	}))
	f.llm.errs[llm.AgentProducer] = llm.ErrNoAPIKey

	first := f.handle(ctx, id)
	second := f.handle(ctx, id)

	require.Error(t, first)
	require.Error(t, second)
	assert.Contains(t, first.Error(), planadvice.DraftNote(1))
	assert.Contains(t, second.Error(), planadvice.DraftNote(1), "the second run still tells the draft")
	w, err := world.World(ctx, id, "")
	require.NoError(t, err)
	require.Len(t, w.Characters, 1, "and does not make another")
	assert.Equal(t, img.ID, w.Characters[0].FaceReferenceAssetID)
	assert.True(t, w.Characters[0].Draft)
}
