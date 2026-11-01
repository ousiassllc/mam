package app

import (
	"context"
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/prompttranslate"
	"github.com/ousiassllc/mam/backend/internal/config"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
)

// newGentest builds the generation test service and registers its job handler on jobs (before the queue starts and
// before Recover, which fails the jobs of a kind without handler). A slot assignment of an import goes through the
// World service, so its locks and kind checks apply.
func newGentest(cfg config.Config, db *repository.DB, jobs *jobqueue.Queue, adapter *modeladapter.Adapter, services *service.Services, translator *prompttranslate.Translator, logger *slog.Logger) *gentest.Service {
	g := gentest.New(gentest.Deps{
		Repo: gentestrepo.New(db), Jobs: jobs, JobTable: repository.NewJobRepository(db), Adapter: adapter,
		DataDir: cfg.DataDir, AssetsDir: cfg.AssetsDir(),
		Projects: repository.NewProjectRepository(db), Assets: repository.NewAssetRepository(db), References: repository.NewReferenceRepository(db),
		Assign: func(ctx context.Context, projectID string, target domain.WorldTarget, id, slot, referenceID string) error {
			_, err := services.World.AssignReference(ctx, projectID, "", target, id, slot, referenceID)
			return err
		},
		Explain: func(err error) string {
			_, msg := mmview.Refuse(err)
			return msg
		},
		Hidden:     services.ModelManager.HidesModel,
		Translator: translator,
		Logger:     logger,
	})
	g.Register()
	return g
}

// newTranslator is the translation of the generation test's prompts: it reasons through the route of the Prompt Planner in
// the "LLM の割り当て" (llm.json), and names its primary target in the estimate. nil when the settings have no such route.
func newTranslator(c llm.Completer, cfg llm.Config) *prompttranslate.Translator {
	route, err := cfg.RouteFor(prompttranslate.Agent)
	if err != nil {
		return nil
	}
	return prompttranslate.New(c, route.Primary)
}
