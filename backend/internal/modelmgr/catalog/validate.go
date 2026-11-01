package catalog

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

var (
	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// A model file is pinned to a commit of a Hugging Face repository (never a branch such as main).
	modelURLPattern = regexp.MustCompile(`^https://huggingface\.co/[A-Za-z0-9._-]+/[A-Za-z0-9._-]+/resolve/[0-9a-f]{40}/[^?#\s]+$`)
	engineTag       = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	fileNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*\.safetensors$`)
	// sourceNoteDate finds the date a mirror's SHA-256 was checked against the original, in a File.SourceNote.
	sourceNoteDate = regexp.MustCompile(`20[0-9]{2}-[0-9]{2}-[0-9]{2}`)
)

// hasCheckDate reports whether the note names a real calendar date (YYYY-MM-DD).
func hasCheckDate(note string) bool {
	for _, d := range sourceNoteDate.FindAllString(note, -1) {
		if _, err := time.Parse(time.DateOnly, d); err == nil {
			return true
		}
	}
	return false
}

// unspecifiedLicense is the license ID of a model whose source states no license text.
const unspecifiedLicense = "unspecified"

// modelFolders are the ComfyUI model folders a catalog file goes to. latent_upscale_models is the folder of the spatial upscaler
// the LTX-2.5 workflow loads (LatentUpscaleModelLoader).
var modelFolders = []string{"checkpoints", "diffusion_models", "text_encoders", "vae", "loras", "latent_upscale_models"}

// loaderInputs are the inputs of the core loader nodes that name a model file, with the folder the file is in.
var loaderInputs = map[string]map[string]string{
	"CheckpointLoaderSimple":   {"ckpt_name": "checkpoints"},
	"UNETLoader":               {"unet_name": "diffusion_models"},
	"CLIPLoader":               {"clip_name": "text_encoders"},
	"DualCLIPLoader":           {"clip_name1": "text_encoders", "clip_name2": "text_encoders"},
	"VAELoader":                {"vae_name": "vae"},
	"LoraLoaderModelOnly":      {"lora_name": "loras"},
	"LatentUpscaleModelLoader": {"model_name": "latent_upscale_models"},
}

// Validate checks the catalog: the pins (https URL at a commit or tag, SHA-256, size), the licenses, the
// consent rules and that every workflow of a model is a valid template whose loader nodes name exactly the
// files of the model. It returns every problem found; nil means the catalog can be used.
func Validate(c *Catalog, template func(id string) (localcfg.Template, bool)) []error {
	errs := append(validateEngine(c.Engine), validateRuntimes(c.Runtimes)...)
	seenModel, seenWorkflow := map[string]bool{}, map[string]bool{}
	shared := map[string]File{}
	for _, m := range c.Models {
		where := "model " + m.ID
		if seenModel[m.ID] {
			errs = append(errs, fmt.Errorf("%s: duplicate id", where))
		}
		seenModel[m.ID] = true
		errs = append(errs, validateModel(m, shared)...)
		for _, w := range m.Workflows {
			if seenWorkflow[w.Name] {
				errs = append(errs, fmt.Errorf("%s: workflow name %q is used twice", where, w.Name))
			}
			seenWorkflow[w.Name] = true
			errs = append(errs, validateWorkflow(m, w, template)...)
		}
	}
	return errs
}

func validateLicense(where string, l License) []error {
	var errs []error
	if l.ID == "" || l.Version == "" || l.Name == "" || strings.TrimSpace(l.Summary) == "" {
		errs = append(errs, fmt.Errorf("%s: the license needs id, version, name and summary", where))
	}
	if !strings.HasPrefix(l.URL, "https://") {
		errs = append(errs, fmt.Errorf("%s: the license url must be https", where))
	}
	return errs
}

func validateEngine(e Engine) []error {
	var errs []error
	if !engineTag.MatchString(e.Version) {
		errs = append(errs, fmt.Errorf("engine: version %q is not a release tag", e.Version))
	}
	if e.SourceURL != "https://github.com/Comfy-Org/ComfyUI/tree/"+e.Version {
		errs = append(errs, fmt.Errorf("engine: source_url must be the source of the pinned tag"))
	}
	for name, p := range map[string]string{"archive_root": e.ArchiveRoot, "python": e.Python, "main": e.Main} {
		if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "..") || strings.Contains(p, `\`) {
			errs = append(errs, fmt.Errorf("engine: %s %q must be a relative slash path", name, p))
		}
	}
	errs = append(errs, validateLicense("engine", e.License)...)
	for _, v := range e.VariantList() {
		a, ok := e.Variants[v]
		where := fmt.Sprintf("engine variant %s", v)
		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("%s: missing", where))
			continue
		case a.URL != fmt.Sprintf("https://github.com/Comfy-Org/ComfyUI/releases/download/%s/ComfyUI_windows_portable_%s.7z", e.Version, v):
			errs = append(errs, fmt.Errorf("%s: url must be the release asset of the pinned tag", where))
		case !sha256Pattern.MatchString(a.SHA256):
			errs = append(errs, fmt.Errorf("%s: sha256 must be 64 lower-case hex digits", where))
		case a.Size <= 0 || a.ExtractedSize <= a.Size || a.Label == "":
			errs = append(errs, fmt.Errorf("%s: size, extracted_size (larger) and label are required", where))
		}
	}
	if len(e.Variants) != len(e.VariantList()) {
		errs = append(errs, fmt.Errorf("engine: unknown variants"))
	}
	return errs
}

func validateModel(m Model, shared map[string]File) []error {
	where := "model " + m.ID
	var errs []error
	if !localcfg.ValidName(m.ID) || m.Name == "" || strings.TrimSpace(m.Description) == "" {
		errs = append(errs, fmt.Errorf("%s: id (slug), name and description are required", where))
	}
	if m.Kind != localcfg.MediaImage && m.Kind != localcfg.MediaVideo {
		errs = append(errs, fmt.Errorf("%s: kind must be image or video", where))
	}
	if m.Tier != TierStandard && m.Tier != TierAdvanced {
		errs = append(errs, fmt.Errorf("%s: tier must be standard or advanced", where))
	}
	if m.VRAM.MinGB <= 0 || strings.TrimSpace(m.VRAM.Source) == "" {
		errs = append(errs, fmt.Errorf("%s: vram needs min_gb and the source of the guideline", where))
	}
	errs = append(errs, validateLicense(where, m.License)...)
	restricted := !m.License.CommercialUse || m.License.CommercialCondition != "" || len(m.License.ExcludedRegions) > 0
	if m.ConsentRequired != (m.Tier == TierAdvanced) || (restricted && !m.ConsentRequired) {
		errs = append(errs, fmt.Errorf("%s: an advanced model, and a model whose license is not plain commercial use, requires consent (and only those)", where))
	}
	errs = append(errs, validateRating(where, m)...)
	if m.RequiresHFToken {
		// The files of a login-only model stay at the Hugging Face repository that asks for the login: a mirror of a third
		// party would be a way round the gate (and the install checks the access against that repository).
		for _, f := range m.Files {
			if f.SourceNote != "" {
				errs = append(errs, fmt.Errorf("%s: file %s: a model behind a Hugging Face login is not mirrored by a third party", where, f.Name))
			}
		}
	}
	if len(m.Files) == 0 || len(m.Workflows) == 0 {
		errs = append(errs, fmt.Errorf("%s: files and workflows are required", where))
	}
	return append(errs, validateFiles(where, m.Files, shared)...)
}

// validateRating checks the content rating and what follows from it: an adult model is an advanced one (the consent
// shows the adult notice after the license summary), a license without a text (id "unspecified") is not commercial
// use, and trigger words belong to a model that carries a LoRA.
func validateRating(where string, m Model) []error {
	var errs []error
	if m.ContentRating != RatingGeneral && m.ContentRating != RatingAdult {
		errs = append(errs, fmt.Errorf("%s: content_rating must be general or adult", where))
	}
	if m.Adult() && m.Tier != TierAdvanced {
		errs = append(errs, fmt.Errorf("%s: an adult model is an advanced model (consent required)", where))
	}
	if m.License.ID == unspecifiedLicense && m.License.CommercialUse {
		errs = append(errs, fmt.Errorf("%s: a license without a text (%s) is not commercial use", where, unspecifiedLicense))
	}
	hasLoRA := slices.ContainsFunc(m.Files, func(f File) bool { return f.Folder == "loras" })
	if len(m.TriggerWords) > 0 && !hasLoRA {
		errs = append(errs, fmt.Errorf("%s: trigger_words belong to a model with a LoRA file", where))
	}
	if slices.ContainsFunc(m.TriggerWords, func(w string) bool { return strings.TrimSpace(w) == "" }) {
		errs = append(errs, fmt.Errorf("%s: a trigger word is empty", where))
	}
	return errs
}

// validateFiles checks the pins of every file of a model; shared maps a destination to the file first seen there.
func validateFiles(where string, files []File, shared map[string]File) []error {
	var errs []error
	names := map[string]bool{}
	for _, f := range files {
		fw := where + " file " + f.Name
		switch {
		case !fileNamePattern.MatchString(f.Name):
			errs = append(errs, fmt.Errorf("%s: name must be a .safetensors file name", fw))
		case !slices.Contains(modelFolders, f.Folder):
			errs = append(errs, fmt.Errorf("%s: folder %q is not a model folder", fw, f.Folder))
		case f.SourceNote != "" && !hasCheckDate(f.SourceNote):
			errs = append(errs, fmt.Errorf("%s: source_note must say where the original is and the date (YYYY-MM-DD) its SHA-256 was checked", fw))
		case !modelURLPattern.MatchString(f.URL):
			errs = append(errs, fmt.Errorf("%s: url must be https://huggingface.co/<repo>/resolve/<40-hex commit>/<path>", fw))
		case !sha256Pattern.MatchString(f.SHA256):
			errs = append(errs, fmt.Errorf("%s: sha256 must be 64 lower-case hex digits", fw))
		case f.Size <= 0:
			errs = append(errs, fmt.Errorf("%s: size is required", fw))
		case names[f.Folder+"/"+f.Name]:
			errs = append(errs, fmt.Errorf("%s: listed twice", fw))
		}
		names[f.Folder+"/"+f.Name] = true
		if other, ok := shared[f.Path()]; ok && (other.SHA256 != f.SHA256 || other.Size != f.Size) {
			errs = append(errs, fmt.Errorf("%s: the same destination with another sha256 or size in another model", fw))
		}
		shared[f.Path()] = f
	}
	return errs
}

func validateWorkflow(m Model, w Workflow, template func(id string) (localcfg.Template, bool)) []error {
	where := fmt.Sprintf("model %s workflow %s", m.ID, w.Name)
	t, ok := template(w.Template)
	switch {
	case !localcfg.ValidName(w.Name):
		return []error{fmt.Errorf("%s: name must be a slug", where)}
	case !ok:
		return []error{fmt.Errorf("%s: template %q does not exist", where, w.Template)}
	case t.Media != m.Kind:
		return []error{fmt.Errorf("%s: template %s makes %s, the model is %s", where, t.ID, t.Media, m.Kind)}
	}
	endpoints := map[string]localcfg.Endpoint{"bundled-comfyui": {Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:8188"}}
	var errs []error
	for _, e := range localcfg.ValidateModel("bundled-comfyui/"+w.Name, t.Model, endpoints) {
		errs = append(errs, fmt.Errorf("%s: %w", where, e))
	}
	graph, err := localcfg.ParseGraph(t.Model.Workflow.Graph)
	if err != nil {
		return append(errs, fmt.Errorf("%s: %w", where, err))
	}
	used := map[string]bool{}
	for id, n := range graph {
		for input, folder := range loaderInputs[n.ClassType] {
			var name string
			if json.Unmarshal(n.Inputs[input], &name) != nil {
				errs = append(errs, fmt.Errorf("%s: node %s (%s) has no file name in %s", where, id, n.ClassType, input))
				continue
			}
			if !slices.ContainsFunc(m.Files, func(f File) bool { return f.Folder == folder && f.Name == name }) {
				errs = append(errs, fmt.Errorf("%s: node %s loads %s/%s, which is not a file of the model", where, id, folder, name))
			}
			used[folder+"/"+name] = true
		}
	}
	for _, f := range m.Files {
		if !used[f.Folder+"/"+f.Name] {
			errs = append(errs, fmt.Errorf("%s: the file %s/%s is not loaded by the workflow", where, f.Folder, f.Name))
		}
	}
	return errs
}
