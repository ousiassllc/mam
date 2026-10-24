package workflows

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
	"github.com/ousiassllc/mam/backend/internal/repository/comfywf"
)

// snapshot is every version that can run, as models of the endpoint the workflow names. A version whose graph or
// assignment does not make a valid model (no prompt input, a missing output node) is left out: it is a draft.
func snapshot(ws []comfywf.Workflow, vs []comfywf.Version, pinned []string, log *slog.Logger) map[string]localcfg.SavedWorkflow {
	byID := make(map[string]comfywf.Workflow, len(ws))
	for _, w := range ws {
		byID[w.ID] = w
	}
	out := make(map[string]localcfg.SavedWorkflow, len(vs))
	for _, v := range vs {
		w, ok := byID[v.WorkflowID]
		if !ok {
			continue
		}
		spec, err := localcfg.ParseSpec(v.Assignment)
		if err != nil {
			log.Warn("a saved workflow version has unreadable settings", "workflow", w.ID, "version", v.Version, "err", err)
			continue
		}
		model := spec.Model(localcfg.Media(w.Kind), v.APIJSON)
		if len(localcfg.ValidateSaved(model)) > 0 {
			continue
		}
		id := modelreg.WorkflowID(w.ID, v.Version)
		out[id] = localcfg.SavedWorkflow{
			ID: id, WorkflowID: w.ID, Version: v.Version, Name: w.Name, Endpoint: w.Endpoint, Current: v.Version == w.Version, Pinned: slices.Contains(pinned, id), Model: model,
		}
	}
	return out
}

// Publish reads the workflows and hands them to the Model Adapter's loader (localcfg.PublishWorkflows). The app calls
// it before it reads the settings file for the first time: the routes of the file may name a saved workflow.
func Publish(ctx context.Context, repo *comfywf.Repository, log *slog.Logger) error {
	ws, err := repo.List(ctx)
	if err != nil {
		return fmt.Errorf("list workflows: %w", err)
	}
	vs, err := repo.Versions(ctx)
	if err != nil {
		return fmt.Errorf("list workflow versions: %w", err)
	}
	pinned, err := repo.PinnedModels(ctx)
	if err != nil {
		return err
	}
	localcfg.PublishWorkflows(snapshot(ws, vs, pinned, log))
	return nil
}

// refresh publishes the workflows and has the Model Adapter read the settings again, so that a saved change
// is a choice and runs at once. A settings file that cannot be read is logged: the workflow is saved either way.
func (s *Service) refresh(ctx context.Context) error {
	if err := Publish(ctx, s.Repo, s.Logger); err != nil {
		return err
	}
	if s.Models != nil {
		if err := s.Models.Reload(); err != nil {
			s.Logger.Warn("reload the model settings after a workflow change failed", "err", err)
		}
	}
	return nil
}
