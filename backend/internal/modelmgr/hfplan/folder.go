package hfplan

import (
	"path"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf"
)

// Folders are the folders below data/models/ that a file can be installed into: the five the catalog uses, which the
// core loader nodes read (checkpoints, UNETLoader, CLIPLoader, VAELoader, LoraLoaderModelOnly). Other folders (ControlNet,
// upscalers, ...) are not offered: the import places files for those loaders only.
var Folders = []string{"checkpoints", "diffusion_models", "text_encoders", "vae", "loras"}

// ValidFolder reports whether f is one of Folders.
func ValidFolder(f string) bool { return slices.Contains(Folders, f) }

// FolderFromPath is the folder a directory of the repository path names (the nearest directory to the file wins):
// "split_files/text_encoders/x.safetensors" is text_encoders, "vae/ae.safetensors" is vae. "" when no directory is one.
func FolderFromPath(p string) string {
	dir := path.Dir(p)
	for dir != "." && dir != "/" && dir != "" {
		if base := path.Base(dir); ValidFolder(base) {
			return base
		}
		dir = path.Dir(dir)
	}
	return ""
}

// Families of a checkpoint the header tells apart. Only FamilySDXL has workflow templates.
const (
	FamilySDXL        = "sdxl"
	FamilySDXLRefiner = "sdxl-refiner"
	FamilySD1         = "sd1"
	FamilySD2         = "sd2"
)

// How a folder was decided.
const (
	SourcePath   = "path"
	SourceHeader = "header"
	SourceUser   = "user"
)

// Inference is what the header of a .safetensors file says.
type Inference struct {
	// Folder is the guessed folder ("" when the tensor names match none of the known layouts).
	Folder string `json:"folder,omitempty"`
	// Family is the model family of an all-in-one checkpoint or a LoRA ("" when not known).
	Family string `json:"family,omitempty"`
	// Reason says which names decided it (for the user).
	Reason string `json:"reason,omitempty"`
}

// Infer guesses the folder and family from the tensor names and the __metadata__ of a .safetensors header. The table is
// the one measured on real files (Issue #842); anything it does not cover is Folder "" and the user chooses.
func Infer(h hf.Header) Inference {
	s := scan(h.Names)
	meta := strings.ToLower(h.Metadata["modelspec.architecture"] + " " + h.Metadata["ss_base_model_version"])
	switch {
	case s.lora:
		return Inference{Folder: "loras", Family: familyFromMeta(meta), Reason: "LoRA のテンソル名（lora_unet_・lora_te・lora_A/B・lora_down/up・.alpha）があるため"}
	case s.diffusionModel && s.cond && s.firstStage:
		return Inference{Folder: "checkpoints", Family: checkpointFamily(s, meta), Reason: "拡散モデル・テキストエンコーダー・VAE が 1 つにまとまった一体型のため"}
	case s.diffusionModel:
		return Inference{Folder: "diffusion_models", Reason: "model.diffusion_model.* だけがあり、テキストエンコーダー・VAE を含まないため"}
	case s.textEncoder:
		return Inference{Folder: "text_encoders", Reason: "言語モデルのテンソル名（text_model.・encoder.block.・model.layers.）のため"}
	case s.vae:
		return Inference{Folder: "vae", Reason: "encoder.*・decoder.*（と quant_conv）だけがあるため"}
	case s.transformer:
		return Inference{Folder: "diffusion_models", Reason: "transformer のテンソル名（double_blocks.・joint_blocks.・blocks.N.）のため"}
	}
	return Inference{Family: familyFromMeta(meta)}
}

type tensorScan struct {
	lora, diffusionModel, cond, firstStage, textEncoder, vae, transformer bool
	sdxlEmbedder1, embedder0Model, sd1Cond                                bool
}

func scan(names []string) tensorScan {
	var s tensorScan
	var enc, dec, other bool
	for _, n := range names {
		switch {
		case strings.HasPrefix(n, "lora_unet_"), strings.HasPrefix(n, "lora_te"), strings.HasPrefix(n, "lora_transformer_"),
			strings.Contains(n, ".lora_A."), strings.Contains(n, ".lora_B."), strings.Contains(n, ".lora_down."),
			strings.Contains(n, ".lora_up."), strings.HasSuffix(n, ".alpha"):
			s.lora = true
		case strings.HasPrefix(n, "model.diffusion_model."):
			s.diffusionModel = true
		case strings.HasPrefix(n, "conditioner."):
			s.cond = true
			s.sdxlEmbedder1 = s.sdxlEmbedder1 || strings.HasPrefix(n, "conditioner.embedders.1.")
			s.embedder0Model = s.embedder0Model || strings.HasPrefix(n, "conditioner.embedders.0.model.")
		case strings.HasPrefix(n, "cond_stage_model."):
			s.cond = true
			s.sd1Cond = s.sd1Cond || strings.HasPrefix(n, "cond_stage_model.transformer.")
		case strings.HasPrefix(n, "first_stage_model."):
			s.firstStage = true
		case strings.HasPrefix(n, "text_model."), strings.HasPrefix(n, "encoder.block."), strings.HasPrefix(n, "model.layers."):
			s.textEncoder = true
		case strings.HasPrefix(n, "double_blocks."), strings.HasPrefix(n, "joint_blocks."), strings.HasPrefix(n, "single_blocks."), blockN(n):
			s.transformer = true
		case strings.HasPrefix(n, "encoder."):
			enc = true
		case strings.HasPrefix(n, "decoder."):
			dec = true
		case strings.HasPrefix(n, "quant_conv."), strings.HasPrefix(n, "post_quant_conv."):
		default:
			other = true
		}
	}
	s.vae = enc && dec && !other
	return s
}

// blockN matches "blocks.<digits>." (a transformer without a prefix).
func blockN(n string) bool {
	rest, ok := strings.CutPrefix(n, "blocks.")
	if !ok {
		return false
	}
	i := strings.IndexByte(rest, '.')
	if i <= 0 {
		return false
	}
	for _, r := range rest[:i] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func checkpointFamily(s tensorScan, meta string) string {
	switch {
	case s.sdxlEmbedder1:
		return FamilySDXL
	case s.embedder0Model:
		return FamilySDXLRefiner
	case s.sd1Cond:
		return FamilySD1
	case s.cond && strings.Contains(meta, "stable-diffusion-xl"):
		return FamilySDXL
	}
	return ""
}

// familyFromMeta reads the family a LoRA or a checkpoint states in its metadata (modelspec.architecture, ss_base_model_version).
func familyFromMeta(meta string) string {
	switch {
	case strings.Contains(meta, "stable-diffusion-xl"), strings.Contains(meta, "sdxl"):
		return FamilySDXL
	case strings.Contains(meta, "stable-diffusion-v1"), strings.Contains(meta, "sd_v1"), strings.Contains(meta, "sd1"):
		return FamilySD1
	}
	return ""
}
