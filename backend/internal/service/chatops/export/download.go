package export

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
)

// Download is the Handler of OpDownloadExport: it changes nothing, so it is carried out at once (Spec.Immediate) and never asks.
type Download struct{ deps Deps }

// NewDownload returns the handler of OpDownloadExport. Only Deps.View is used.
func NewDownload(d Deps) *Download { return &Download{deps: d} }

// Question is never asked for an Immediate operation.
func (d *Download) Question(context.Context, domain.Project, commandinterpreter.Operation) (chatops.Card, error) {
	return chatops.Card{}, chatops.ErrNoChange
}

// Apply answers with the latest export of the Project: a notice with the same download button the chat draws after an export (ref_type export). Without an export it says
// so and offers to make one.
func (d *Download) Apply(ctx context.Context, project domain.Project, _ commandinterpreter.Operation) (chatops.Result, error) {
	view, err := d.deps.View(ctx, project.ID)
	if err != nil {
		return chatops.Result{}, err
	}
	latest, ok := latestExport(view.Exports)
	if !ok {
		return chatops.Result{Text: "まだ書き出した動画がありません。「この動画を書き出して」と送ると書き出します（ffmpeg で書き出すので時間がかかります）。"}, nil
	}
	label := latest.AspectRatio + " / " + latest.Resolution + " / " + latest.Codec
	return chatops.Result{
		Text: "最新の書き出し（" + latest.ExportedAt.Local().Format("2006-01-02 15:04") + "・" + label + "）です。下のボタンからダウンロードできます。",
		Messages: []domain.ChatMessage{{
			ProjectID: project.ID, Role: domain.RoleAssistant, Kind: domain.KindText,
			Content: "書き出した動画（" + label + "）",
			RefType: domain.RefExport, RefID: latest.ID,
		}},
	}, nil
}

// latestExport is the newest export; the list is newest first, but the answer does not depend on it.
func latestExport(exports []domain.Export) (domain.Export, bool) {
	var latest domain.Export
	found := false
	for _, e := range exports {
		if !found || e.ExportedAt.After(latest.ExportedAt) {
			latest, found = e, true
		}
	}
	return latest, found
}

// Bundle is the Handler of OpExportBundle: it changes nothing and needs no service (the route of the bundle prepares it when the link is opened).
type Bundle struct{}

// Question is never asked for an Immediate operation.
func (Bundle) Question(context.Context, domain.Project, commandinterpreter.Operation) (chatops.Card, error) {
	return chatops.Card{}, chatops.ErrNoChange
}

// Apply answers with the link of the bundle: the ZIP of the Project's data and asset files that the settings screen's 「バンドルを書き出す」 gives.
func (Bundle) Apply(_ context.Context, project domain.Project, _ commandinterpreter.Operation) (chatops.Result, error) {
	return chatops.Result{Text: "プロジェクトのバンドル（ZIP: データと素材ファイル）はこちらからダウンロードできます: " + BundlePath(project.ID) +
		"\n別の端末では、プロジェクト一覧の「プロジェクトバンドルを読み込む」から復元できます。"}, nil
}
