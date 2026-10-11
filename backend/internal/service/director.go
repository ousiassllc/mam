package service

import (
	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/repair"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// DirectorService runs DIRECTOR MODE (F-02, F-05, F-24): the AUTO ⇄ DIRECTOR switch, the
// AUTO / MANUAL / LOCK setting of parameters, the manual edit of the Canonical Shot Spec and the
// Command Interpreter that turns a direction into operations on one Shot.
type DirectorService struct {
	*core
	gens     *repository.GenerationRepository
	locks    *repository.LockRepository
	versions *repository.VersionRepository
	// orch starts the regeneration of a Shot; nil when generation is not configured.
	orch *generation.Orchestrator
	// interpreter reads directions; nil when no Reasoning LLM is configured.
	interpreter *commandinterpreter.Interpreter
	// branch makes the Branches of a "create_branch" operation; set by New.
	branch *BranchService
	// takes finds the Take a Partial Regeneration fixes; repair makes it (nil without ffmpeg, LLM
	// or Model Adapter).
	takes  *repository.TakeRepository
	repair *repair.Agent
	// judges gives the Inspector the latest Judge result of each Take.
	judges *repository.JudgeRepository
	// brands are the Safety / Brand Constraints checked before a regeneration is queued.
	brands *repository.BrandRepository
}
