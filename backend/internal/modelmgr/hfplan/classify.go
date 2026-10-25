// Package hfplan decides what the model manager does with the files of a Hugging Face repository (spec/architecture/
// model-manager/model-manager.md §13): which can be installed, into which ComfyUI folder, under which name, and
// whether the GPU is likely to hold them. It reads through package hf and never downloads a model file.
package hfplan

import (
	"path"
	"regexp"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf"
)

// Format is what the extension of a file says about how ComfyUI reads it.
type Format string

// The formats the import tells apart.
const (
	// FormatSafetensors (.safetensors, .sft) is the only format that is installed.
	FormatSafetensors Format = "safetensors"
	// FormatGGUF is listed but not selectable: ComfyUI reads it only with the custom node ComfyUI-GGUF.
	FormatGGUF Format = "gguf"
	// FormatPickle (.ckpt .pt .pt2 .pth .bin .pkl) can run arbitrary code when it is loaded: refused.
	FormatPickle Format = "pickle"
	// FormatOther is anything else (README, config, images): not a model file, not listed.
	FormatOther Format = "other"
)

// FormatOf is the format of the file by its extension (case-insensitive).
func FormatOf(p string) Format {
	switch strings.ToLower(path.Ext(p)) {
	case ".safetensors", ".sft":
		return FormatSafetensors
	case ".gguf":
		return FormatGGUF
	case ".ckpt", ".pt", ".pt2", ".pth", ".bin", ".pkl":
		return FormatPickle
	}
	return FormatOther
}

// The reasons a file cannot be installed (shown to the user).
const (
	ReasonGGUF     = "ComfyUI-GGUF（カスタムノード）が要るため、同梱の ComfyUI では使えません"
	ReasonPickle   = "pickle 形式（.ckpt・.pt・.pth・.bin・.pkl など）は、読み込むときに任意のコードが動きうるため導入しません。.safetensors を選んでください"
	ReasonSplit    = "分割された形式（diffusers の unet/・text_encoder/・transformer/ や -00001-of-0000N のファイル）は、ComfyUI の単一ファイルではないため導入できません。一体型の .safetensors を選んでください"
	ReasonNoLFS    = "LFS のファイルではなく SHA-256 が分からないため、モデルファイルとしては扱いません"
	ReasonUnsafe   = "Hugging Face のセキュリティスキャンが unsafe と判定したため導入できません"
	ReasonNotModel = "モデルのファイルではありません"
	ReasonEmpty    = "ファイルの大きさが 0、またはツリーの大きさと LFS の大きさが一致しません"
)

var (
	shardPattern = regexp.MustCompile(`(?i)-[0-9]{5}-of-[0-9]{5}\.`)
	// diffusersFile is the file name a diffusers component folder uses.
	diffusersFile = regexp.MustCompile(`(?i)^(diffusion_pytorch_model|pytorch_model|model)(\.[a-z0-9]+)?(-[0-9]{5}-of-[0-9]{5})?\.(safetensors|sft)$`)
	// diffusersDirs are the component folders of a diffusers pipeline.
	diffusersDirs = map[string]bool{
		"unet": true, "transformer": true, "text_encoder": true, "text_encoder_2": true, "text_encoder_3": true,
		"vae": true, "safety_checker": true, "image_encoder": true, "prior": true, "controlnet": true,
	}
)

// Split reports whether the path is a piece of a split layout: a numbered shard (model-00001-of-00004) or the weights
// file of a diffusers component folder (unet/diffusion_pytorch_model.safetensors). ComfyUI loads neither as one file.
func Split(p string) bool {
	if shardPattern.MatchString(p) {
		return true
	}
	dir, name := path.Split(p)
	if dir == "" || !diffusersFile.MatchString(name) {
		return false
	}
	return diffusersDirs[path.Base(strings.TrimSuffix(dir, "/"))]
}

// Verdict says whether a file can be installed and, if not, why.
type Verdict struct {
	Installable bool   `json:"installable"`
	Reason      string `json:"reason,omitempty"`
}

// Judge decides a file of a tree: its format, the LFS SHA-256 it needs, the layout and the scan. security is the status of
// FileInfo ("" when not read).
func Judge(e hf.TreeEntry, security string) Verdict {
	switch FormatOf(e.Path) {
	case FormatGGUF:
		return Verdict{Reason: ReasonGGUF}
	case FormatPickle:
		return Verdict{Reason: ReasonPickle}
	case FormatOther:
		return Verdict{Reason: ReasonNotModel}
	}
	switch {
	case security == hf.SecurityUnsafe:
		return Verdict{Reason: ReasonUnsafe}
	case Split(e.Path):
		return Verdict{Reason: ReasonSplit}
	case e.SHA256 == "":
		return Verdict{Reason: ReasonNoLFS}
	case e.Size <= 0 || e.LFSSize != e.Size:
		return Verdict{Reason: ReasonEmpty}
	}
	return Verdict{Installable: true}
}

// variantToken finds the part of a file name that says which version of the model it is: the precision (bf16, fp16,
// fp8_e4m3fn, int8_convrot, ...) or the GGUF quantization (Q4_K_M, IQ3_XS).
var variantToken = regexp.MustCompile(`(?i)(^|[._-])(bf16|fp16|fp32|fp8([_-][a-z0-9]+)*|int8([_-][a-z0-9]+)*|nf4|f16|f32|i?q[0-9]([_-][a-z0-9]+)*)($|[._-])`)

// Group splits a file name into the model it belongs to and its version: "flux1-dev-fp8_e4m3fn.safetensors" is the group
// "flux1-dev" and the variant "fp8_e4m3fn"; "ae.safetensors" has no variant. Files of one group are shown together.
func Group(p string) (group, variant string) {
	name := path.Base(p)
	stem := strings.TrimSuffix(name, path.Ext(name))
	loc := variantToken.FindStringSubmatchIndex(stem)
	if loc == nil {
		return stem, ""
	}
	variant = stem[loc[4]:loc[5]]
	rest := stem[:loc[4]] + stem[loc[5]:]
	rest = strings.Trim(rest, "._-")
	if rest == "" {
		rest = stem
	}
	return rest, variant
}
