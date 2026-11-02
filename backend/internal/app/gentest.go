package app

import (
	"context"
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/agent/imagematch"
	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/prompttranslate"
	"github.com/ousiassllc/mam/backend/internal/config"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/facesim"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/match"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/poses"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/nodegate"
)

// newGentest builds the generation test service and registers its job handler on jobs (before the queue starts and
// before Recover, which fails the jobs of a kind without handler). A slot assignment of an import goes through the
// World service, so its locks and kind checks apply.
func newGentest(cfg config.Config, db *repository.DB, jobs *jobqueue.Queue, adapter *modeladapter.Adapter, services *service.Services, translator *prompttranslate.Translator, matcher *imagematch.Matcher, logger *slog.Logger) *gentest.Service {
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
		Hidden:          services.ModelManager.HidesModel,
		MissingNodes:    nodegate.ForModels(services.ModelManager.Nodes, modelmanager.BundledEndpoint), // a workflow whose custom node is not installed cannot be chosen
		BundledEndpoint: modelmanager.BundledEndpoint,
		Translator:      translator,
		Poses:           newPoseLibrary(logger),
		Logger:          logger,
	})
	g.Register()
	match.New(match.Deps{Gentest: g, Matcher: matcher}) // the runner of the prompt match is found again with match.From
	return g
}

// newPoseLibrary is the pose library (Shisei) of the generation test, below the localai directory next to the executable like the local
// runtimes. nil when the directory cannot be found or the library cannot be opened: the generation test then has no "ポーズを選ぶ".
func newPoseLibrary(logger *slog.Logger) *poses.Library {
	exe, err := executablePath()
	if err != nil {
		logger.Warn("pose library: the executable was not found", "err", err)
		return nil
	}
	lib, err := poses.New(poses.Deps{Root: store.LocalAIRoot(exe)})
	if err != nil {
		logger.Warn("pose library: open failed", "err", err)
		return nil
	}
	return lib
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

// newMatcher is the LLM work of the prompt match: it writes and fixes the prompts through the route of the Prompt Planner and
// compares the pictures through the route of the AI Video Judge. nil when the settings have no such route. faces and mix say whether
// and how the face of the generated image is mixed into the similarity (the "match_face" of the Judge setting, off by default).
func newMatcher(c llm.Completer, cfg llm.Config, faces facesim.Engine, mix imagematch.FaceMix) *imagematch.Matcher {
	create, err := cfg.RouteFor(imagematch.PromptAgent)
	if err != nil {
		return nil
	}
	judge, err := cfg.RouteFor(imagematch.JudgeAgent)
	if err != nil {
		return nil
	}
	return imagematch.New(c, create.Primary, judge.Primary).UseFaces(faces, mix)
}
