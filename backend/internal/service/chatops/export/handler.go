package export

import (
	"context"
	"net/url"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
)

// View is what the export operations read of the Project: the same EditorView the editor panel draws its export button from.
type View struct {
	State domain.ProjectState
	// CanRender is false without ffmpeg.
	CanRender bool
	// HasAudio and AudioCurrent say whether the series has an Audio Timeline and whether it was made for its latest Edit Timeline (an export is refused unless it was).
	HasAudio, AudioCurrent bool
	// Defaults are the options preselected: the Brief's aspect ratio, 1080p, H.264.
	Defaults domain.ExportOptions
	// Exports are the exports made, newest first.
	Exports []domain.Export
}

// Done is the export a run made.
type Done struct {
	ExportID string
	Options  domain.ExportOptions
	State    domain.ProjectState
}

// Deps is what the operations need of the service layer. Export is EditorService.Export (the screen and the MCP tool export_video); it reports what the screen reports as a
// *chatops.Refusal (a state that cannot export, no audio, no ffmpeg).
type Deps struct {
	View   func(ctx context.Context, projectID string) (View, error)
	Export func(ctx context.Context, projectID string, in Args) (Done, error)
}

// Render is the Handler of OpExportVideo.
type Render struct{ deps Deps }

// NewRender returns the handler of OpExportVideo.
func NewRender(d Deps) *Render { return &Render{deps: d} }

const noFFmpeg = "ffmpeg / ffprobe が見つからないため、音声と書き出しは使えません。設定画面で ffmpeg を設定してください。"

// refuseUnexportable says why the Project cannot be exported now, as the editor panel does: no ffmpeg, a state that is not READY / EXPORTED, no audio (or not of the latest edit).
func refuseUnexportable(v View) error {
	switch {
	case !v.State.CanExport():
		return chatops.Refuse("いまは書き出せません（現在の状態: %s）。書き出せるのは Final QC に合格した後（READY）です。編集・音声・Final QC を済ませてから、もう一度お願いします。", v.State)
	case !v.CanRender:
		return chatops.Refuse("%s", noFFmpeg)
	case !v.HasAudio:
		return chatops.Refuse("この系列にはまだ音声がないため、書き出せません。")
	case !v.AudioCurrent:
		return chatops.Refuse("この系列の音声が最新の編集に対応していないため、書き出せません。音声を作り直してください。")
	}
	return nil
}

// effective is the options the export would use (what the director named over the defaults), checked as the service checks them.
func effective(defaults domain.ExportOptions, a Args) (domain.ExportOptions, error) {
	opts := defaults
	if a.AspectRatio != "" {
		opts.AspectRatio = a.AspectRatio
	}
	if a.Resolution != "" {
		opts.Resolution = a.Resolution
	}
	if a.Codec != "" {
		opts.Codec = a.Codec
	}
	switch {
	case !slices.Contains(domain.ExportAspectRatios(), opts.AspectRatio):
		return opts, chatops.Refuse("アスペクト比は %s から選んでください", strings.Join(domain.ExportAspectRatios(), " / "))
	case !slices.Contains(domain.ExportResolutions(), opts.Resolution):
		return opts, chatops.Refuse("解像度は %s から選んでください", strings.Join(domain.ExportResolutions(), " / "))
	case !slices.Contains(domain.ExportCodecs(), opts.Codec):
		return opts, chatops.Refuse("コーデックは %s から選んでください", strings.Join(domain.ExportCodecs(), " / "))
	}
	return opts, nil
}

// Question says what the export does and with which options. It refuses (and posts no card) when the Project cannot be exported now.
func (r *Render) Question(ctx context.Context, project domain.Project, op commandinterpreter.Operation) (chatops.Card, error) {
	var a Args
	if err := op.DecodeArgs(&a); err != nil {
		return chatops.Card{}, err
	}
	view, err := r.deps.View(ctx, project.ID)
	if err != nil {
		return chatops.Card{}, err
	}
	if err := refuseUnexportable(view); err != nil {
		return chatops.Card{}, err
	}
	opts, err := effective(view.Defaults, a)
	if err != nil {
		return chatops.Card{}, err
	}
	// the yes button carries the options the card shows, so that a default the Brief changes meanwhile does not change what was agreed to
	chosen, err := commandinterpreter.NewOperation(OpExportVideo, Args{AspectRatio: opts.AspectRatio, Resolution: opts.Resolution, Codec: opts.Codec})
	if err != nil {
		return chatops.Card{}, err
	}
	text := "動画を書き出しますね？\n" +
		"・縦横比 " + choice(opts.AspectRatio, view.Defaults.AspectRatio) + " / 解像度 " + choice(opts.Resolution, view.Defaults.Resolution) + " / コーデック " + choice(opts.Codec, view.Defaults.Codec) + "\n" +
		"・ffmpeg で書き出します（時間がかかります。終わるまでこのチャットの返事は待ちます）。\n"
	if view.State == domain.StateExported {
		text += "・すでに書き出し済みです。もう一度書き出します（前の書き出しは残ります）。\n"
	} else {
		text += "・書き出すと Project は EXPORTED になります。\n"
	}
	return chatops.Card{Text: text + "・終わると、ダウンロードのリンクを出します。", Op: &chosen}, nil
}

// choice is an option with what the Brief's default is when it is not that.
func choice(chosen, byDefault string) string {
	if chosen == byDefault {
		return chosen + "（既定）"
	}
	return chosen + "（既定: " + byDefault + "）"
}

// Apply renders the video through EditorService.Export. That call posts its own notice with the download button (ref_type export), so the answer is short and gives the
// path of the file besides.
func (r *Render) Apply(ctx context.Context, project domain.Project, op commandinterpreter.Operation) (chatops.Result, error) {
	var a Args
	if err := op.DecodeArgs(&a); err != nil {
		return chatops.Result{}, err
	}
	done, err := r.deps.Export(ctx, project.ID, a)
	if err != nil {
		return chatops.Result{}, err
	}
	text := "書き出しが完了しました（" + done.Options.AspectRatio + " / " + done.Options.Resolution + " / " + done.Options.Codec + "）。Project は " + string(done.State) + " です。"
	if done.ExportID != "" {
		text += "\nダウンロード: " + VideoPath(project.ID, done.ExportID)
	}
	return chatops.Result{Text: text}, nil
}

// VideoPath is the app's own route of an exported video (GET /projects/:id/exports/:exportId/video), the one the chat's export notice links to.
func VideoPath(projectID, exportID string) string {
	return "/projects/" + url.PathEscape(projectID) + "/exports/" + url.PathEscape(exportID) + "/video"
}

// BundlePath is the app's own route of the Project bundle (GET /projects/:id/bundle).
func BundlePath(projectID string) string { return "/projects/" + url.PathEscape(projectID) + "/bundle" }
