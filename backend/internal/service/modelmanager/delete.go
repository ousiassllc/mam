package modelmanager

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
)

// DeleteModel removes an installed model: its models of the managed endpoint and its files. A file that another
// installed model uses too (a VAE, a text encoder) stays while that model is installed (the count is over the
// files recorded at the installs). A model that an image purpose or a Quality Policy chooses is refused
// (*InvalidError): change the assignment first. An install that is running is ErrBusy.
func (s *Service) DeleteModel(ctx context.Context, modelID string) error {
	m, ok := s.cat.Model(modelID)
	if !ok {
		return ErrNotFound
	}
	if s.activeJob("model:"+modelID) != "" {
		return ErrBusy
	}
	st := s.state.Get()
	inst, installed := st.Models[modelID]
	if !installed {
		return ErrNotInstalled
	}
	if err := s.checkNotInUse(m.Workflows); err != nil {
		return err
	}
	for _, name := range inst.Workflows {
		err := s.Models.DeleteModel(ctx, BundledEndpoint, name)
		if err != nil && !errors.Is(err, localmodels.ErrNotFound) {
			return err
		}
	}
	keep := map[string]bool{}
	for id, other := range st.Models {
		if id == modelID {
			continue
		}
		for _, f := range other.Files {
			keep[f.Path] = true
		}
	}
	var failed error
	for _, f := range inst.Files {
		if keep[f.Path] {
			continue
		}
		dest := s.Layout.ModelPath(f.Path)
		for _, p := range []string{dest, fetch.PartPath(dest)} {
			if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
				failed = errors.Join(failed, err)
			}
		}
	}
	if err := s.state.Update(func(st *store.State) { delete(st.Models, modelID) }); err != nil {
		return err
	}
	return failed
}

// checkNotInUse refuses a delete when a model of the workflows is chosen somewhere.
func (s *Service) checkNotInUse(workflows []catalog.Workflow) error {
	view, err := s.Models.View()
	if err != nil {
		return err
	}
	var used []string
	for _, mv := range view.Models {
		if mv.Endpoint != BundledEndpoint || len(mv.UsedBy) == 0 {
			continue
		}
		for _, w := range workflows {
			if mv.Name == w.Name {
				used = append(used, mv.ID+"（"+strings.Join(mv.UsedBy, "、")+"）")
			}
		}
	}
	if len(used) > 0 {
		return invalid("このモデルは使われています: %s。先に割り当てを変えてください", strings.Join(used, "、"))
	}
	return nil
}
