// Package export is the chat operation of the finished video (#1140; spec/components/chat-settings-operations.md §5): the director asks for "この動画を書き出して" and gets a
// confirmation card (the export re-renders the video with ffmpeg and moves the Project to EXPORTED, so it is not done before the answer); "さっきの書き出しをダウンロードしたい"
// and "プロジェクトをバンドルで書き出したい" change nothing and are answered at once with the link of the app's own download route. The render is EditorService.Export, the same
// function the editor panel (POST /projects/:id/export) and the MCP tool export_video call.
package export

import (
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// The operations of this package.
const (
	// OpExportVideo renders the finished video (MCP export_video, POST /projects/:id/export). It asks for confirmation.
	OpExportVideo commandinterpreter.OpType = "export_video"
	// OpDownloadExport answers with the download link of the latest export (MCP get_export_download_url). It changes nothing.
	OpDownloadExport commandinterpreter.OpType = "get_export_download_url"
	// OpExportBundle answers with the download link of the Project bundle (MCP export_project_bundle, GET /projects/:id/bundle). It changes nothing.
	OpExportBundle commandinterpreter.OpType = "export_project_bundle"
)

// Args of OpExportVideo: the choices the director named; a field left out takes the Brief's default (the aspect ratio of the Brief, 1080p, H.264).
type Args struct {
	AspectRatio string `json:"aspect_ratio,omitempty"`
	Resolution  string `json:"resolution,omitempty"`
	Codec       string `json:"codec,omitempty"`
}

func init() {
	commandinterpreter.Register(OpExportVideo, commandinterpreter.Spec{
		Example: `{"type": "export_video", "args": {"aspect_ratio": "9:16", "resolution": "1080p", "codec": "h264"}}`,
		Doc: `- "export_video": the director wants the finished video rendered (exported) now ("この動画を書き出して", "縦長の 720p で書き出したい", "完成した動画をエクスポートして"). ` +
			`Optional "args.aspect_ratio" (one of ` + quoted(domain.ExportAspectRatios()) + `), "args.resolution" (` + quoted(domain.ExportResolutions()) + `) and "args.codec" (` + quoted(domain.ExportCodecs()) + `); ` +
			`leave out what the director does not name (the Brief's defaults apply). The chat asks for confirmation first. Never use it to download a video that was already exported.` + "\n",
		Clean: cleanRender, Asked: AsksRender, Detect: detectRender,
		AnyMode: true, Confirm: true,
	})
	commandinterpreter.Register(OpDownloadExport, commandinterpreter.Spec{
		Example: `{"type": "get_export_download_url"}`,
		Doc: `- "get_export_download_url": the director wants to download a video that was already exported ("さっきの書き出しをダウンロードしたい", "書き出した動画をダウンロード"). ` +
			`Nothing is rendered again; it takes no args.` + "\n",
		Clean: keep, Asked: AsksDownload, Detect: detectDownload,
		AnyMode: true, Immediate: true,
	})
	commandinterpreter.Register(OpExportBundle, commandinterpreter.Spec{
		Example: `{"type": "export_project_bundle"}`,
		Doc: `- "export_project_bundle": the director wants the whole Project as one ZIP bundle (the data and the asset files) to download or back up ("プロジェクトをバンドルで書き出したい", "バックアップの ZIP が欲しい"). ` +
			`It takes no args.` + "\n",
		Clean: keep, Asked: AsksBundle, Detect: detectBundle,
		AnyMode: true, Immediate: true,
	})
}

func quoted(values []string) string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = `"` + v + `"`
	}
	return strings.Join(out, ", ")
}

// keep is the Clean of the operations without args: the model's args, if it wrote any, are dropped.
func keep(op commandinterpreter.Operation) (commandinterpreter.Operation, bool) {
	return commandinterpreter.Operation{Type: op.Type}, true
}

// cleanRender writes the choices of the model the way the service reads them ("9：16" is "9:16", "4k" is "2160p", "hevc" is "h265"). A value that is none of the choices is kept
// as it is: the card then refuses it with the choices to pick from, instead of exporting what the director did not ask for.
func cleanRender(op commandinterpreter.Operation) (commandinterpreter.Operation, bool) {
	var a Args
	if err := op.DecodeArgs(&a); err != nil {
		return commandinterpreter.Operation{}, false
	}
	a = Args{AspectRatio: aspectOf(a.AspectRatio), Resolution: resolutionOf(a.Resolution), Codec: codecOf(a.Codec)}
	out, err := commandinterpreter.NewOperation(OpExportVideo, a)
	return out, err == nil
}

func aspectOf(s string) string {
	return strings.NewReplacer("：", ":", " ", "", "　", "").Replace(strings.TrimSpace(s))
}

func resolutionOf(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "4k", "uhd":
		return "2160p"
	case "2k", "qhd":
		return "1440p"
	case "fhd", "フルhd":
		return "1080p"
	case "720", "1080", "1440", "2160":
		return s + "p"
	}
	return s
}

func codecOf(s string) string {
	s = strings.NewReplacer(".", "", "-", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(s)))
	switch s {
	case "avc", "mp4":
		return domain.CodecH264
	case "hevc":
		return domain.CodecH265
	case "webm":
		return domain.CodecVP9
	}
	return s
}
