package modelmanager

import (
	"os"
	"slices"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

// ModelState is where a model of the catalog stands.
type ModelState string

// The states of a model.
const (
	ModelNotInstalled ModelState = "not_installed"
	ModelInstalling   ModelState = "installing"
	ModelInstalled    ModelState = "installed"
	// ModelIncomplete: recorded as installed but a file is missing or has another size (deleted by hand).
	ModelIncomplete ModelState = "incomplete"
)

// FileView is a file of a model.
type FileView struct {
	Name   string `json:"name"`
	Folder string `json:"folder"`
	Size   int64  `json:"size"`
	// Present: the file is in place (its size is right; its SHA-256 was checked at the install).
	Present bool `json:"present"`
	// SharedWith lists the other installed models that use the same file: it stays while one of them is installed.
	SharedWith []string `json:"shared_with,omitempty"`
}

// ModelView is a model of the catalog for the list.
type ModelView struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Kind        localcfg.Media `json:"kind"`
	Tier        catalog.Tier   `json:"tier"`
	Description string         `json:"description"`
	Files       []FileView     `json:"files"`
	TotalSize   int64          `json:"total_size"`
	// NeedBytes is the free space an install needs (what is not downloaded yet).
	NeedBytes int64 `json:"need_bytes"`
	// VRAM is the 目安 of the video memory (VRAM.Source says where from; Estimate: mam's own figure, not run on a GPU).
	VRAM    catalog.VRAM    `json:"vram"`
	License catalog.License `json:"license"`
	// ConsentRequired: the license has to be accepted before the install. Accepted: it was, in this version.
	ConsentRequired bool       `json:"consent_required"`
	Accepted        bool       `json:"accepted"`
	AcceptedAt      *time.Time `json:"accepted_at,omitempty"`
	RequiresHFToken bool       `json:"requires_hf_token"`
	State           ModelState `json:"state"`
	// InstallJob is the install that is queued or running.
	InstallJob string `json:"install_job,omitempty"`
	// ModelIDs are the #831 identifiers of the models an install creates (comfyui:bundled-comfyui/<name>), the
	// ones a Shot's lock and the image purposes choose.
	ModelIDs []string `json:"model_ids"`
	// WorkflowCheck is "構造テストのみ・GPU 実機未検証": the workflows were not run on a GPU.
	WorkflowCheck string `json:"workflow_check"`
}

// workflowCheck is shown with every workflow: they were checked for structure only (no GPU on the development host).
const workflowCheck = "構造テストのみ・GPU 実機未検証"

// Catalog lists the models of the catalog with their state, in the order standard images, standard videos, then the
// advanced ones that need a license acceptance.
func (s *Service) Catalog() []ModelView {
	st := s.state.Get()
	views := make([]ModelView, 0, len(s.cat.Models))
	for _, m := range s.cat.Models {
		views = append(views, s.view(m, st))
	}
	return views
}

// Model is one model of the catalog.
func (s *Service) Model(id string) (ModelView, error) {
	m, ok := s.cat.Model(id)
	if !ok {
		return ModelView{}, ErrNotFound
	}
	return s.view(m, s.state.Get()), nil
}

func (s *Service) view(m catalog.Model, st store.State) ModelView {
	v := ModelView{
		ID: m.ID, Name: m.Name, Kind: m.Kind, Tier: m.Tier, Description: m.Description, TotalSize: m.TotalSize(), VRAM: m.VRAM,
		License: m.License, ConsentRequired: m.ConsentRequired, RequiresHFToken: m.RequiresHFToken, WorkflowCheck: workflowCheck,
		Files: make([]FileView, 0, len(m.Files)), ModelIDs: []string{},
	}
	if c, ok := st.Consents[m.License.ID]; ok && c.Version == m.License.Version {
		v.Accepted = true
		at := c.AcceptedAt
		v.AcceptedAt = &at
	}
	present := 0
	for _, f := range m.Files {
		ok := fileIsThere(s.Layout.ModelPath(f.Path()), f.Size)
		if ok {
			present++
		}
		fv := FileView{Name: f.Name, Folder: f.Folder, Size: f.Size, Present: ok, SharedWith: sharedWith(st, m.ID, f.Path())}
		v.Files = append(v.Files, fv)
		if !ok {
			v.NeedBytes += f.Size - partSize(s.Layout.ModelPath(f.Path()), f.Size)
		}
	}
	for _, w := range m.Workflows {
		v.ModelIDs = append(v.ModelIDs, localcfg.ID(localcfg.KindComfyUI, BundledEndpoint, w.Name))
	}
	switch _, installed := st.Models[m.ID]; {
	case s.activeJob("model:"+m.ID) != "":
		v.State, v.InstallJob = ModelInstalling, s.activeJob("model:"+m.ID)
	case installed && present == len(m.Files):
		v.State = ModelInstalled
	case installed:
		v.State = ModelIncomplete
	default:
		v.State = ModelNotInstalled
	}
	return v
}

// sharedWith lists the other installed models that hold the file at path.
func sharedWith(st store.State, self, path string) []string {
	var ids []string
	for id, im := range st.Models {
		if id != self && slices.ContainsFunc(im.Files, func(f store.InstalledFile) bool { return f.Path == path }) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

func fileIsThere(path string, size int64) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular() && st.Size() == size
}

func partSize(dest string, total int64) int64 {
	if st, err := os.Stat(fetch.PartPath(dest)); err == nil {
		return min(st.Size(), total)
	}
	return 0
}
