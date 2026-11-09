package loraimport

import (
	"context"
	"errors"
	"os"
	"slices"
	"sort"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

// List is the LoRAs the user added, newest first.
func (s *Service) List() []Model {
	st := s.State.Get()
	out := make([]Model, 0, len(st.LocalLoRAs))
	for _, l := range st.LocalLoRAs {
		out = append(out, Model{LocalLoRA: l, Present: present(s.Layout.ModelPath(l.Path), l.Size)})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].AddedAt.Equal(out[j].AddedAt) {
			return out[i].AddedAt.After(out[j].AddedAt)
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func present(file string, size int64) bool {
	st, err := os.Stat(file)
	return err == nil && st.Mode().IsRegular() && st.Size() == size
}

// ByName is the record of the LoRA the engine lists under the file name (nil record: a file the user did not add through mam).
func (s *Service) ByName(name string) (store.LocalLoRA, bool) {
	l, ok := s.State.Get().LocalLoRAs[relOf(name)]
	return l, ok
}

// Update replaces what the user confirmed about a LoRA (the file is not touched). An unknown path is ErrNotFound.
func (s *Service) Update(_ context.Context, rel string, m Meta) (Model, error) {
	families, words, err := m.check()
	if err != nil {
		return Model{}, err
	}
	var out store.LocalLoRA
	var found bool
	if err := s.State.Update(func(st *store.State) {
		old, ok := st.LocalLoRAs[rel]
		if !ok {
			return
		}
		found = true
		out = recordOf(old.Path, old.Name, old.SHA256, old.Size, old.OriginalName, m, families, words, old.AddedAt)
		st.LocalLoRAs[rel] = out
	}); err != nil {
		return Model{}, err
	}
	if !found {
		return Model{}, ErrNotFound
	}
	return Model{LocalLoRA: out, Present: present(s.Layout.ModelPath(out.Path), out.Size)}, nil
}

// Delete removes the record of a LoRA the user added, and its file unless a catalog model or a file from Hugging Face is recorded at the same path (a file
// two records hold stays while the other does). A file nobody recorded (one the user put in the folder by hand, one a catalog model installed) is never touched:
// an unknown path is ErrNotFound.
func (s *Service) Delete(_ context.Context, rel string) error {
	st := s.State.Get()
	l, ok := st.LocalLoRAs[rel]
	if !ok {
		return ErrNotFound
	}
	unlock := s.Lock(rel)
	defer unlock()
	if !heldByOthers(st, rel) {
		if err := os.Remove(s.Layout.ModelPath(l.Path)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return s.State.Update(func(st *store.State) { delete(st.LocalLoRAs, rel) })
}

// heldByOthers says a catalog model or a file from Hugging Face is recorded at the path.
func heldByOthers(st store.State, rel string) bool {
	if _, ok := st.UserModels[rel]; ok {
		return true
	}
	for _, im := range st.Models {
		if slices.ContainsFunc(im.Files, func(f store.InstalledFile) bool { return f.Path == rel }) {
			return true
		}
	}
	return false
}
