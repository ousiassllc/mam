package app

import (
	"context"
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/agent/imagematch"
	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/prompttranslate"
	"github.com/ousiassllc/mam/backend/internal/app/facewire"
	"github.com/ousiassllc/mam/backend/internal/config"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/facesim/holder"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/repository/reciperepo"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/facefusionsettings"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/face"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/match"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/poses"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/videoenhance"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/exposedgate"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/nodegate"
)

// newGentest builds the generation test service and registers its job handler on jobs (before the queue starts and
// before Recover, which fails the jobs of a kind without handler). A slot assignment of an import goes through the
// World service, so its locks and kind checks apply.
func newGentest(cfg config.Config, db *repository.DB, jobs *jobqueue.Queue, adapter *modeladapter.Adapter, services *service.Services, translator *prompttranslate.Translator, matcher *imagematch.Matcher, faces *holder.Holder, video videoenhance.Wiring, logger *slog.Logger) *gentest.Service {
	swap, swapCheck := face.Swapper(facefusionsettings.Default(cfg.DataDir)) // FaceFusion, the external face swapper (#995)
	g := gentest.New(gentest.Deps{
		Repo: gentestrepo.New(db), Recipes: reciperepo.New(db), Jobs: jobs, JobTable: repository.NewJobRepository(db), Adapter: adapter,
		DataDir: cfg.DataDir, AssetsDir: cfg.AssetsDir(),
		Projects: repository.NewProjectRepository(db), Assets: repository.NewAssetRepository(db), References: repository.NewReferenceRepository(db),
		Assign: func(ctx context.Context, projectID string, target domain.WorldTarget, id, slot, referenceID string) error {
			_, err := services.World.AssignReference(ctx, projectID, "", target, id, slot, referenceID)
			return err
		},
		Explain: mmview.Failure, Hidden: services.ModelManager.HidesModel,
		HiddenWith: exposedgate.Hides(services.ModelManager), HideChoices: exposedgate.Choices(services.ModelManager),
		MissingNodes:    nodegate.ForModels(services.ModelManager.Nodes, modelmanager.BundledEndpoint), // a workflow whose custom node is not installed cannot be chosen
		CatalogInfo:     mmview.InfoOf(services.ModelManager),                                          // the catalog's adult flag and traits, for the filter of the model list
		PlatformWarning: mmview.PlatformWarningFor(services.ModelManager),
		BundledEndpoint: modelmanager.BundledEndpoint,
		LoRAMeta:        mmview.LoRAMeta(services.ModelManager), // the base model, strength, trigger words and license of the LoRAs the user added (#1119)
		Translator:      translator,
		Poses:           newPoseLibrary(services.ModelManager.AdultContentEnabled, logger),
		FaceCrop:        face.Crop(faces),
		FaceScore:       face.Score(faces), FaceOff: face.OffNow(faces.Off), // FaceOff: why there is no score right now; the models can be installed while mam runs (#1026)
		FaceModels: facewire.Models(cfg, faces, logger),
		FaceFusion: swap, FaceFusionCheck: swapCheck, Cloud: video.Cloud, Probe: video.Probe, // the paid cloud and the ffprobe of "make a video larger / smoother" (#1031)
		Logger: logger,
	})
	g.Register()
	match.New(match.Deps{Gentest: g, Matcher: matcher}) // the runner of the prompt match is found again with match.From
	return g
}

// newPoseLibrary is the pose library (Shisei) of the generation test, below the localai directory next to the executable like the local runtimes.
// adultOpen is the age gate of the model manager: the suggestive series (sexy, duo) are installed and shown only while it is open. nil when it cannot be opened.
func newPoseLibrary(adultOpen func() bool, logger *slog.Logger) *poses.Library {
	exe, err := executablePath()
	if err != nil {
		logger.Warn("pose library: the executable was not found", "err", err)
		return nil
	}
	lib, err := poses.New(poses.Deps{Root: store.LocalAIRoot(exe), AdultOpen: adultOpen})
	if err != nil {
		logger.Warn("pose library: open failed", "err", err)
		return nil
	}
	return lib
}
