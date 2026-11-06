package overview

import (
	"context"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex"
	"github.com/ousiassllc/mam/backend/internal/service/facefusionsettings"
	"github.com/ousiassllc/mam/backend/internal/service/ffmpegsettings"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/companion"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// A line of the companion software is its state in words and in the data-state of its row; both are read from the owner of the software now (nothing is stored).
const (
	readText      = "使えます"
	unreadable    = "設定ファイルを読めません"
	ffmpegMissing = "見つかりません"
	faceUnset     = "未設定"
)

// ffmpegLine is how ffmpeg and ffprobe stand: found by the lookup with the saved paths and the downloaded copy, or not.
func ffmpegLine(svc *ffmpegsettings.Service) (string, string) {
	v, err := svc.View()
	switch {
	case err != nil:
		return unreadable, companion.StateAttention
	case v.FFmpeg.Err != nil || v.FFprobe.Err != nil:
		return ffmpegMissing, companion.StateMissing
	}
	return readText, companion.StateInstalled
}

// faceFusionLine is how FaceFusion stands: found (its files are read; it is not run) or not set up yet. Not having it is the usual state: it is installed only by
// the one who uses the face swap.
func faceFusionLine(svc *facefusionsettings.Service) (string, string) {
	v, err := svc.View()
	switch {
	case err != nil:
		return unreadable, companion.StateAttention
	case v.Ready():
		return readText, companion.StateInstalled
	}
	return faceUnset, companion.StateMissing
}

// codexLine is how the engine of the Codex sign-in stands (it is installed from the sign-in of ChatGPT, in the settings).
func codexLine(svc *codex.Service) (string, string) {
	st := svc.Status()
	switch st.Engine {
	case codex.EngineInstalling:
		return "導入中", companion.StateInstalling
	case codex.EngineFailed:
		return "エンジンが起動できませんでした", companion.StateAttention
	case codex.EngineReady, codex.EngineRunning:
		if st.Account.SignedIn {
			return "導入済み（ログイン済み）", companion.StateInstalled
		}
		return "導入済み（未ログイン）", companion.StateInstalled
	}
	return "未導入", companion.StateMissing
}

// companionLine is the line of the category "付随ソフト" on the overview: ffmpeg and FaceFusion as they stand, each only when this build has it. State is full (all of
// them usable), partial (FaceFusion is not set up: the usual state, it is installed by the one who needs it) or attention (ffmpeg is missing, or a settings file cannot be read).
func companionLine(d Deps) (string, string) {
	var parts []string
	state := view.StateFull
	if d.FFmpeg != nil {
		text, st := ffmpegLine(d.FFmpeg)
		parts = append(parts, "ffmpeg: "+text)
		if st != companion.StateInstalled {
			state = view.StateAttention
		}
	}
	if d.FaceFusion != nil {
		text, st := faceFusionLine(d.FaceFusion)
		parts = append(parts, "FaceFusion: "+text)
		switch {
		case st == companion.StateAttention:
			state = view.StateAttention
		case st != companion.StateInstalled && state == view.StateFull:
			state = view.StatePartial
		}
	}
	return strings.Join(parts, "・"), state
}

// Companion is the line of the category "付随ソフト" for a card that only has the two services (the overview of the settings screen): the words and the data-state
// of the overview of the model manager.
func Companion(ffmpeg *ffmpegsettings.Service, faceFusion *facefusionsettings.Service) (string, string) {
	return companionLine(Deps{FFmpeg: ffmpeg, FaceFusion: faceFusion})
}

// CompanionIndex is the list of the companion software: what it is for, where it is installed and how it stands, in the order of the spec. A row is left out when this
// build lacks the part (its service is nil); the ones of other categories link to the place the software is installed in.
func CompanionIndex(ctx context.Context, d Deps) companion.Index {
	var rows []companion.Row
	add := func(tool, name, purpose, href string, line func() (string, string)) {
		text, state := line()
		rows = append(rows, companion.Row{Tool: tool, Name: name, Purpose: purpose, Href: href, State: state, Label: text})
	}
	if d.FFmpeg != nil {
		add(companion.ToolFFmpeg, "ffmpeg・ffprobe", "動画の書き出し・プレビュー・サムネイル・Judge", paths.FFmpegSettings(), func() (string, string) { return ffmpegLine(d.FFmpeg) })
	}
	if d.FaceFusion != nil {
		add(companion.ToolFaceFusion, "FaceFusion", "生成した画像の顔の入れ替え（任意）", paths.FaceFusionSettings(), func() (string, string) { return faceFusionLine(d.FaceFusion) })
	}
	if d.Face != nil {
		add(companion.ToolFaceModels, "顔チェック用モデル（YuNet・SFace）", "顔の類似度の測定", paths.ModelManagerFaceModels(), func() (string, string) { return face(d.Face) })
	}
	if d.Svc != nil {
		add(companion.ToolComfyUI, "ComfyUI エンジン", "画像・動画の生成", paths.ModelManagerCategory(paths.MMComfyUI)+"#mm-engine", func() (string, string) { return engine(ctx, d.Svc) })
	}
	if d.Runtimes != nil {
		add(companion.ToolLocalRuntime, "ローカル音声・口パク", "GPT-SoVITS・Irodori-TTS・MuseTalk", paths.ModelManagerCategory(paths.MMLocalRuntimes)+"#mm-runtimes", func() (string, string) { return runtimes(ctx, d.Runtimes) })
	}
	if d.Nodes != nil {
		add(companion.ToolCustomNodes, "追加ノード", "ComfyUI のカスタムノード", paths.ModelManagerCategory(paths.MMCustomNodes)+"#mm-nodes", func() (string, string) { return customNodes(ctx, d.Nodes) })
	}
	if d.Codex != nil {
		add(companion.ToolCodex, "Codex エンジン（ChatGPT でログイン）", "ChatGPT 経由のテキスト生成", paths.SettingsCategory(paths.SettingsAPIKeys), func() (string, string) { return codexLine(d.Codex) })
	}
	return companion.Index{Rows: rows, UpdatedAt: time.Now().Format(time.TimeOnly)}
}
