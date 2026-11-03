package wfrefresh

import (
	"context"
	"errors"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
)

// Creator is the part of localmodels.Service that AddMissing uses.
type Creator interface {
	CreateModel(ctx context.Context, in localmodels.ModelInput) error
}

// Refresh is what mam does at the start for the models of the managed endpoint: AddMissing, then Run (so that a workflow added now is
// brought up to date in the same start when its template is ahead of the copy; a copy made from the template now is not behind).
func Refresh(ctx context.Context, models interface {
	Models
	Creator
}, rec Record, cat *catalog.Catalog, endpoint string, warn func(msg string, args ...any)) {
	AddMissing(ctx, models, rec, cat, endpoint, warn)
	Run(ctx, models, cat, endpoint, warn)
}

// Record is the install record of the model manager (state.json): what each installed catalog model installed. AddWorkflows
// appends names to the workflows of the installed model id, so that the delete and the age gate cover them too.
type Record interface {
	InstalledModels() map[string]store.InstalledModel
	AddWorkflows(id string, names []string) error
}

// AddMissing creates the workflows that the catalog got after a model was installed (#990: sdxl-base-inpaint and flux1-dev-inpaint, added by
// #974): an install saves the workflows the catalog then lists, and Run only brings the contents of the existing ones up to date, so
// an installed model would show the new workflow only after it was installed again. A workflow is added to an installed model when its name
// is not in the model's record and every file the catalog lists for the model is installed as the record says (a workflow of a changed
// file list is not made for a model that lacks the file; installing it again does). A model of that name that exists already (the user made
// it, or the record lost it) is never overwritten: the user's copy stays and the name is recorded. A name that the record has and that the user
// deleted is not made again. warn gets a failure; a pinned model file (ErrPinned) is not one.
func AddMissing(ctx context.Context, models Creator, rec Record, cat *catalog.Catalog, endpoint string, warn func(msg string, args ...any)) {
	installed := rec.InstalledModels()
	for _, m := range cat.Models {
		inst, ok := installed[m.ID]
		if !ok || !hasFiles(inst, m) {
			continue
		}
		var added []string
		for _, w := range m.Workflows {
			if slices.Contains(inst.Workflows, w.Name) {
				continue
			}
			model, err := w.Model(localcfg.TemplateByID)
			if err != nil {
				warn("make the new workflow of an installed model failed", "model", w.Name, "err", err)
				continue
			}
			err = models.CreateModel(ctx, localmodels.ModelInput{Endpoint: endpoint, Name: w.Name, Model: model})
			switch {
			case errors.Is(err, localmodels.ErrPinned):
				return
			case err != nil && !errors.Is(err, localmodels.ErrExists):
				warn("add the new workflow to an installed model failed", "model", w.Name, "err", err)
				continue
			}
			added = append(added, w.Name)
		}
		if len(added) == 0 {
			continue
		}
		if err := rec.AddWorkflows(m.ID, added); err != nil {
			warn("record the new workflows of an installed model failed", "model", m.ID, "err", err)
		}
	}
}

// hasFiles says the record has every file of the catalog model with the catalog's SHA-256.
func hasFiles(inst store.InstalledModel, m catalog.Model) bool {
	for _, f := range m.Files {
		if !slices.ContainsFunc(inst.Files, func(i store.InstalledFile) bool { return i.Path == f.Path() && i.SHA256 == f.SHA256 }) {
			return false
		}
	}
	return true
}
