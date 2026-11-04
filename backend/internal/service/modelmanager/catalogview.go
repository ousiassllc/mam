package modelmanager

import (
	"slices"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/diskfile"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/filestate"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/nodegate"
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
	SHA256 string `json:"sha256"` // the digest the file is checked against at the install
	// Present: the file is in place (its size is right; its SHA-256 was checked at the install).
	Present bool `json:"present"`
	// SharedWith lists the other installed models that use the same file: it stays while one of them is installed.
	SharedWith []string `json:"shared_with,omitempty"`
	// SourceNote says where the original of a third party's mirror lies and when its SHA-256 was checked to be the same.
	SourceNote string `json:"source_note,omitempty"`
	Optional   bool   `json:"optional,omitempty"` // the model is installed without this file (a speed mode's LoRA): ModelView.MissingOptional
}

// ModelView is a model of the catalog for the list.
type ModelView struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Kind        localcfg.Media `json:"kind"`
	Tier        catalog.Tier   `json:"tier"`
	Description string         `json:"description"`
	// ContentRating is general or adult. An adult model is listed only while the age gate is open (AdultContent).
	ContentRating catalog.ContentRating `json:"content_rating"`
	// TriggerWords are the words a LoRA of the model needs in the prompt.
	TriggerWords []string   `json:"trigger_words,omitempty"`
	Files        []FileView `json:"files"`
	TotalSize    int64      `json:"total_size"`
	// NeedBytes is the free space an install needs (what is not downloaded yet).
	NeedBytes int64 `json:"need_bytes"`
	// VRAM is the 目安 of the video memory (VRAM.Source says where from; Estimate: mam's own figure, not run on a GPU).
	VRAM    catalog.VRAM    `json:"vram"`
	License catalog.License `json:"license"`
	// ConsentRequired: the license has to be accepted before the install. Accepted: it was, in this version.
	ConsentRequired  bool       `json:"consent_required"`
	Accepted         bool       `json:"accepted"`
	AcceptedAt       *time.Time `json:"accepted_at,omitempty"`
	RequiresHFToken  bool       `json:"requires_hf_token"`
	State            ModelState `json:"state"`
	filestate.Report            // the optional files that are missing, the note and "files in place" (#1025): the keys are the view's own
	// InstallJob is the install that is queued or running.
	InstallJob string `json:"install_job,omitempty"`
	// ModelIDs are the #831 identifiers of the models an install creates (comfyui:bundled-comfyui/<name>), the
	// ones a Shot's lock and the image purposes choose.
	ModelIDs []string `json:"model_ids"`
	// WorkflowCheck is "構造テストのみ・GPU 実機未検証": the workflows were not run on a GPU.
	WorkflowCheck string `json:"workflow_check"`
	// RequiresNodes are the IDs of the custom nodes (the allowlist) the workflows of the model need: it cannot be installed until they are.
	RequiresNodes []string `json:"requires_nodes,omitempty"`
	// AdultNotice is the notice for adult models, which the consent text shows after the license summary (adult models only).
	AdultNotice             string `json:"adult_notice,omitempty"`
	catalog.PlatformColumns        // the OS / GPU generation columns (Issue #979): mmview.PlatformWarning warns from them
}

// workflowCheck is shown with every workflow: they were checked for structure only (no GPU on the development host).
const workflowCheck = "構造テストのみ・GPU 実機未検証"

// Catalog lists the models of the catalog with their state, in the order standard images, standard videos, then the advanced ones that need a license acceptance.
func (s *Service) Catalog() []ModelView {
	st := s.state.Get()
	views := make([]ModelView, 0, len(s.cat.Models))
	for _, m := range s.cat.Models {
		if s.allowed(m) {
			views = append(views, s.view(m, st))
		}
	}
	return views
}

// Model is one model of the catalog. An adult model is ErrAdultNotConfirmed until the age confirmation is on record and "show adult models" is on.
func (s *Service) Model(id string) (ModelView, error) {
	m, ok := s.cat.Model(id)
	if !ok {
		return ModelView{}, ErrNotFound
	}
	if !s.allowed(m) {
		return ModelView{}, ErrAdultNotConfirmed
	}
	return s.view(m, s.state.Get()), nil
}

func (s *Service) view(m catalog.Model, st store.State) ModelView {
	v := ModelView{
		ID: m.ID, Name: m.Name, Kind: m.Kind, Tier: m.Tier, ContentRating: m.ContentRating, Description: m.Description,
		TriggerWords: m.TriggerWords, TotalSize: m.TotalSize(), VRAM: m.VRAM, PlatformColumns: m.PlatformColumns,
		License: m.License, ConsentRequired: m.ConsentRequired, RequiresHFToken: m.RequiresHFToken, WorkflowCheck: workflowCheck,
		Files: make([]FileView, 0, len(m.Files)), ModelIDs: []string{}, RequiresNodes: nodegate.Required(m),
	}
	if m.Adult() {
		v.AdultNotice = AdultNotice
	}
	if c, ok := st.Consents[consentKey(m)]; ok && c.Version == m.License.Version {
		v.Accepted = true
		at := c.AcceptedAt
		v.AcceptedAt = &at
	}
	for _, f := range m.Files {
		ok := diskfile.IsThere(s.Layout.ModelPath(f.Path()), f.Size)
		v.Add(f, ok)
		v.Files = append(v.Files, FileView{Name: f.Name, Folder: f.Folder, Size: f.Size, SHA256: f.SHA256, Present: ok, Optional: f.Optional, SourceNote: f.SourceNote, SharedWith: s.sharedWith(st, m.ID, f.Path())})
		if !ok {
			v.NeedBytes += f.Size - diskfile.PartSize(s.Layout.ModelPath(f.Path()), f.Size)
		}
	}
	for _, w := range m.Workflows {
		v.ModelIDs = append(v.ModelIDs, localcfg.ID(localcfg.KindComfyUI, BundledEndpoint, w.Name))
	}
	// The one judgement of "installed", which the generation test's model list reads too (mmview.InfoOf): recorded, and every required file in place. An install
	// that runs does not change it for such a model (it fetches the optional files into .part files and does not touch the required ones, so the model keeps
	// working: #1025); it only carries the job.
	job := s.activeJob("model:" + m.ID)
	_, recorded := st.Models[m.ID]
	switch {
	case recorded && v.RequiredInPlace():
		v.State = ModelInstalled
	case job != "":
		v.State = ModelInstalling
	case recorded:
		v.State = ModelIncomplete
	default:
		v.State = ModelNotInstalled
	}
	v.InstallJob = job
	v.Seal(v.State == ModelInstalled, v.State != ModelNotInstalled)
	return v
}

// sharedWith lists the other installed models that hold the file at path. An adult model is left out while the age gate is
// closed: the list must not name a model it hides.
func (s *Service) sharedWith(st store.State, self, path string) []string {
	var ids []string
	for id, im := range st.Models {
		if id == self || !slices.ContainsFunc(im.Files, func(f store.InstalledFile) bool { return f.Path == path }) {
			continue
		}
		if other, ok := s.cat.Model(id); ok && !s.allowed(other) {
			continue
		}
		ids = append(ids, id)
	}
	if um, ok := st.UserModels[path]; ok && (!um.NFAA || s.AdultContentEnabled()) { // a file the user added from Hugging Face
		ids = append(ids, "HF: "+um.Name)
	}
	slices.Sort(ids)
	return ids
}
