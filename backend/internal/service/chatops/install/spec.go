// Package install is the chat operation of the installs of the model manager (#1140; spec/components/chat-settings-operations.md §2): a model of the catalog, the ComfyUI
// engine, a .safetensors file of Hugging Face and a local voice / lip-sync runtime. The director says "FLUX.1 schnell を入れて" and gets a card that shows what is fetched
// (size, license, commercial use, what it needs) before anything starts; the yes of the card enqueues the same install the model manager screen and the MCP tools
// install_model, install_model_engine, install_hf_file and install_local_runtime enqueue (modelmanager.Service, localruntime.Service), and a progress card follows.
// An install is global (disk, network, every Project uses it), and the card says so.
package install

import (
	"regexp"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
)

// The operations of this package.
const (
	// OpInstallModel installs a model of the catalog of the model manager.
	OpInstallModel commandinterpreter.OpType = "install_model"
	// OpInstallEngine installs the bundled ComfyUI engine.
	OpInstallEngine commandinterpreter.OpType = "install_model_engine"
	// OpInstallHFFile installs one .safetensors file of a Hugging Face repository.
	OpInstallHFFile commandinterpreter.OpType = "install_hf_file"
	// OpInstallRuntime installs a local voice / lip-sync runtime (GPT-SoVITS, MuseTalk ...).
	OpInstallRuntime commandinterpreter.OpType = "install_local_runtime"
)

// ModelArgs are the arguments of OpInstallModel. Model is what the director said (an id or a name) until the card resolves it: the yes button then carries the id of the
// catalog entry, and with it the license the card showed (the user's acceptance). Pick is set on the button of a card that offered several models: choosing one asks the
// confirmation of that model, it installs nothing.
type ModelArgs struct {
	Model          string `json:"model"`
	LicenseID      string `json:"license_id,omitempty"`
	LicenseVersion string `json:"license_version,omitempty"`
	Pick           bool   `json:"pick,omitempty"`
	// ModelID is only read: a model that writes the MCP name of the argument ("model_id") is understood, and Clean moves it to Model.
	ModelID string `json:"model_id,omitempty"`
}

// EngineArgs are the arguments of OpInstallEngine: the build (what the card resolved, "" for the one of the GPU) and whether to run it on the CPU.
type EngineArgs struct {
	Variant string `json:"variant,omitempty"`
	CPU     bool   `json:"cpu,omitempty"`
}

// HFArgs are the arguments of OpInstallHFFile. Commit and TermsVersion are never read from the model's answer: the card resolves the commit from Hugging Face and the
// terms version it showed, and its yes button carries them.
type HFArgs struct {
	Repo         string `json:"repo"`
	Path         string `json:"path,omitempty"`
	Commit       string `json:"commit,omitempty"`
	TermsVersion string `json:"terms_version,omitempty"`
	// Pick is set on the button of a card that offered several files: choosing one asks the confirmation of that file, it installs nothing.
	Pick bool `json:"pick,omitempty"`
}

// RuntimeArgs are the arguments of OpInstallRuntime: the id or the name of the runtime (the card resolves it to the id).
type RuntimeArgs struct {
	Runtime string `json:"runtime"`
	// Pick is set on the button of a card that offered several runtimes: choosing one asks the confirmation of that one, it installs nothing.
	Pick bool `json:"pick,omitempty"`
}

func init() {
	commandinterpreter.Register(OpInstallModel, commandinterpreter.Spec{
		Example: `{"type": "install_model", "args": {"model": "FLUX.1 schnell"}}`,
		Doc: `- "install_model": the director wants a model of mam's catalog downloaded for local image or video generation ("FLUX.1 schnell を入れて", "Wan 2.2 を導入して"). ` +
			`"args.model" is the id or the name as the director said it; NEVER invent an id: the chat looks it up in the catalog and asks which one when several match. ` +
			`Not for the ComfyUI engine ("install_model_engine"), a voice or lip-sync runtime ("install_local_runtime") or a file of Hugging Face ("install_hf_file").` + "\n",
		Clean: cleanModel, Asked: AsksModel, AnyMode: true, Confirm: true, Global: true,
	})
	commandinterpreter.Register(OpInstallEngine, commandinterpreter.Spec{
		Example: `{"type": "install_model_engine", "args": {}}`,
		Doc: `- "install_model_engine": the director wants the bundled ComfyUI engine installed ("ComfyUI のエンジンを入れて", "ローカル生成の準備をして"). ` +
			`"args.cpu" is true only when the director asks to run it on the CPU; leave "args.variant" out (the chat picks the build of the GPU).` + "\n",
		Clean: cleanEngine, Asked: AsksEngine, AnyMode: true, Confirm: true, Global: true,
	})
	commandinterpreter.Register(OpInstallHFFile, commandinterpreter.Spec{
		Example: `{"type": "install_hf_file", "args": {"repo": "owner/name", "path": "model.safetensors"}}`,
		Doc: `- "install_hf_file": the director wants a .safetensors file of a Hugging Face repository installed ("huggingface.co/owner/name の safetensors を入れて"). ` +
			`"args.repo" is "owner/name" or the huggingface.co URL; "args.path" is the file in it when the director names one. NEVER put a commit, a hash or a license in the answer.` + "\n",
		Clean: cleanHF, Asked: AsksHF, AnyMode: true, Confirm: true, Global: true,
	})
	commandinterpreter.Register(OpInstallRuntime, commandinterpreter.Spec{
		Example: `{"type": "install_local_runtime", "args": {"runtime": "gpt-sovits"}}`,
		Doc: `- "install_local_runtime": the director wants a local voice or lip-sync runtime installed ("GPT-SoVITS を入れて", "MuseTalk を導入して"). ` +
			`"args.runtime" is the id or the name as the director said it ("gpt-sovits", "musetalk", "irodori-tts", "llama-cpp", "ollama").` + "\n",
		Clean: cleanRuntime, Asked: AsksRuntime, AnyMode: true, Confirm: true, Global: true,
	})
}

var (
	verbWord    = regexp.MustCompile(`(?i)(導入|インストール|ダウンロード|入れて|いれて|入れたい|入れ直|入れよう|落として|取得して|セットアップ|\binstall|\bdownload|\bset ?up\b)`)
	modelWord   = regexp.MustCompile(`(?i)(モデル|model|checkpoint|チェックポイント|flux|sdxl|sd3|stable\s*diffusion|qwen|wan\s?2|ltx|hunyuan|chroma|noctq|controlnet|ip-?adapter|esrgan|nomos|アップスケール|upscal)`)
	engineWord  = regexp.MustCompile(`(?i)(エンジン|engine|comfy\s*ui|コンフィ)`)
	hfWord      = regexp.MustCompile(`(?i)(hugging\s?face|ハギングフェイス|ハグフェイス|\bhf\b)`)
	runtimeWord = regexp.MustCompile(`(?i)(ランタイム|runtime|実行環境|gpt-?sovits|sovits|musetalk|irodori|llama\.?cpp|ollama|音声合成|口パク|リップシンク)`)
)

// person is the "モデル" of a fashion model or of the cast ("プロのモデル", "モデルさん"): it names no model of the catalog.
var person = regexp.MustCompile(`(?:プロ|出演者?|ファッション|専属|人物|女性|男性|外国人|日本人|ヘア|ハンド)の?モデル|モデル(?:さん|ちゃん|君|くん|たち|達)`)

// AsksModel reports whether the message asks to install a model: an install word beside a word for a model (or the name of a family of the catalog). A person who is a model
// ("プロのモデルを入れて", "モデルさんを入れて") is not one; the bare 動画 / 画像 / 入れて never suffices.
func AsksModel(message string) bool {
	message = person.ReplaceAllString(message, "")
	return verbWord.MatchString(message) && modelWord.MatchString(message)
}

// AsksEngine reports whether the message asks to install the ComfyUI engine.
func AsksEngine(message string) bool {
	return verbWord.MatchString(message) && engineWord.MatchString(message)
}

// AsksHF reports whether the message asks to install a file of Hugging Face.
func AsksHF(message string) bool { return verbWord.MatchString(message) && hfWord.MatchString(message) }

// AsksRuntime reports whether the message asks to install a local voice or lip-sync runtime.
func AsksRuntime(message string) bool {
	return verbWord.MatchString(message) && runtimeWord.MatchString(message)
}

// maxArgBytes bounds what a name of the model's answer may hold: nothing legitimate is longer, and the card quotes it.
const maxArgBytes = 300

// cleanArgs decodes the arguments of op into a (a pointer to the struct of the type), lets tidy trim them, and encodes the operation again; false when they are unreadable.
func cleanArgs[A any](op commandinterpreter.Operation, tidy func(*A) bool) (commandinterpreter.Operation, bool) {
	var a A
	if err := op.DecodeArgs(&a); err != nil || !tidy(&a) {
		return commandinterpreter.Operation{}, false
	}
	out, err := commandinterpreter.NewOperation(op.Type, a)
	return out, err == nil
}

func fit(s *string) bool {
	*s = strings.TrimSpace(*s)
	return len(*s) <= maxArgBytes
}

func cleanModel(op commandinterpreter.Operation) (commandinterpreter.Operation, bool) {
	return cleanArgs(op, func(a *ModelArgs) bool {
		if a.Model == "" {
			a.Model = a.ModelID
		}
		a.ModelID = ""
		return fit(&a.Model) && fit(&a.LicenseID) && fit(&a.LicenseVersion)
	})
}

func cleanEngine(op commandinterpreter.Operation) (commandinterpreter.Operation, bool) {
	return cleanArgs(op, func(a *EngineArgs) bool {
		a.Variant = strings.ToLower(a.Variant)
		return fit(&a.Variant)
	})
}

func cleanHF(op commandinterpreter.Operation) (commandinterpreter.Operation, bool) {
	return cleanArgs(op, func(a *HFArgs) bool {
		return fit(&a.Repo) && fit(&a.Path) && fit(&a.Commit) && fit(&a.TermsVersion)
	})
}

func cleanRuntime(op commandinterpreter.Operation) (commandinterpreter.Operation, bool) {
	return cleanArgs(op, func(a *RuntimeArgs) bool { return fit(&a.Runtime) })
}
