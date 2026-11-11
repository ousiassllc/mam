package flowtests_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

// modelFake is the image and video backend of every model: no external API is called.
type modelFake struct{}

func (modelFake) GenerateVideo(context.Context, string, modeladapter.VideoRequest) (modeladapter.VideoResult, error) {
	return modeladapter.VideoResult{File: modeladapter.BlobOf(mp4Bytes), MimeType: "video/mp4"}, nil
}

func (modelFake) GenerateImage(context.Context, string, modeladapter.ImageRequest) (modeladapter.ImageResult, error) {
	return modeladapter.ImageResult{Data: pngBytes, MimeType: "image/png"}, nil
}

// newGenerationFixture is a fixture whose Orchestrator starts the generation of a plan by itself (it has the models the start needs): an AUTO Project at VISUAL_DESIGN with
// a plan of three Shots, and the budget the user stated in the chat (a confirmation card) not answered yet.
func newGenerationFixture(t *testing.T, content string) (f pipelineFixture, p domain.Project) {
	t.Helper()
	testenv.Parallel(t)
	db := testdb.Open(t)
	backends := modeladapter.Backends{Image: map[domain.ModelName]modeladapter.ImageBackend{}, Video: map[domain.ModelName]modeladapter.VideoBackend{}}
	for _, m := range domain.ImageModels() {
		backends.Image[m] = modelFake{}
	}
	for _, m := range domain.VideoModels() {
		backends.Video[m] = modelFake{}
	}
	noSleep := func(context.Context, time.Duration) error { return nil }
	jobs := jobqueue.New(repository.NewJobRepository(db), jobqueue.Options{})
	fake := servicetest.NewFakeReasoner()
	assets := filepath.Join(t.TempDir(), "assets")
	svc := servicetest.New(t, service.Deps{
		DB: db, Jobs: jobs, LLM: fake, AssetsDir: assets, FreeBytes: func(string) (uint64, error) { return 1 << 40, nil },
		Models: modeladapter.New(modeladapter.DefaultConfig(), backends, modeladapter.WithSleep(noSleep)),
	})
	ctx, cancel := context.WithCancel(context.Background())
	jobs.Start(ctx)
	t.Cleanup(func() { cancel(); jobs.Stop() })
	f = pipelineFixture{fixture: fixture{svc: svc, db: db, assetsDir: assets}, llm: fake}

	p = f.createProject(t, content)
	projects, plans := repository.NewProjectRepository(db), repository.NewPlanRepository(db)
	require.NoError(t, projects.Transition(ctx, p.ID, domain.StatePlanning))
	require.NoError(t, projects.Transition(ctx, p.ID, domain.StateVisualDesign))
	var plan domain.ScenePlan
	require.NoError(t, json.Unmarshal([]byte(servicetest.FakePlan), &plan))
	var board domain.Storyboard
	require.NoError(t, json.Unmarshal([]byte(servicetest.FakeBoard), &board))
	require.NoError(t, plans.SavePlan(ctx, p.ID, plan, board))
	return f, p
}

// autoStarted reports whether the Orchestrator has started the generation of the plan by itself: it says so in the chat (AUTO is the consent to the start).
func (f pipelineFixture) autoStarted(t testenv.TestingT, projectID string) bool {
	t.Helper()
	for _, m := range f.thread(t, projectID) {
		if m.Role == domain.RoleAssistant && strings.Contains(m.Content, "承認を待たずに生成を始めます") {
			return true
		}
	}
	return false
}

// The Orchestrator waits for the answer to the budget card (nothing paid starts before it). The answer ends the wait whichever it is: a decline (「やめる」) closes the
// card as well as a yes, and the generation goes on, without the budget (#1139).
func TestBudgetCard_EveryAnswerThatClosesTheCardResumesTheGeneration(t *testing.T) {
	for name, reply := range map[string]string{
		"decline": "やめる",
		"accept":  "はい",
	} {
		t.Run(name, func(t *testing.T) {
			f, p := newGenerationFixture(t, "美容液のCMを作りたい。予算は$20で")
			card := operationCards(f.thread(t, p.ID))[0]
			require.True(t, card.Open)
			// Back in AUTO the Orchestrator looks at the Project; once that look is over (waitIdle), "not started" says something about the open card and not about a look that never happened.
			for _, mode := range []domain.Mode{domain.ModeDirector, domain.ModeAuto} {
				_, err := f.svc.Director.SetMode(context.Background(), p.ID, mode)
				require.NoError(t, err)
			}
			f.waitIdle(t, p.ID)
			require.False(t, f.autoStarted(t, p.ID), "nothing is started while the card is open")

			answer(t, f.svc, p.ID, card.ID, reply)

			require.EventuallyWithT(t, func(c *assert.CollectT) {
				assert.True(c, f.autoStarted(c, p.ID), "the Orchestrator looked at the Project again and started the generation")
			}, 10*time.Second, 10*time.Millisecond)
			assert.False(t, operationCards(f.thread(t, p.ID))[0].Open)
		})
	}
}
