// Package appupdate holds the handlers of the "バージョンとアップデート" section of the global settings screen
// (spec/api/endpoints.md). It is a package of its own because handler is at its size limit; like handler,
// it depends on the service layer and the web layer only.
package appupdate

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/appupdate"
	"github.com/ousiassllc/mam/backend/internal/version"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/updatepanel"
)

// devLabel is how a build from a working tree ("dev") shows as the version.
const devLabel = "開発版"

// SectionData is the section as the settings screen shows it. svc is nil in a build that cannot update
// (the headless server): the section then holds the version only.
func SectionData(svc *appupdate.Service) view.Data {
	if svc == nil {
		return view.Data{Version: versionLabel(version.Version)}
	}
	return dataOf(svc.View())
}

// Check runs a check now (POST /settings/update/check) and answers with the section. A check that
// finds and verifies a newer release makes the app quit and install it, so this answer may never arrive.
// A failed check is answered with 200 and the reason in the section: the section is what tells.
func Check(svc *appupdate.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, _ := svc.CheckNow(c.Request.Context()) // Service.Check has logged a failure at ERROR; it shows as the state of the view
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Status(http.StatusOK)
		if err := view.Panel(dataOf(v)).Render(c.Request.Context(), c.Writer); err != nil {
			_ = c.Error(err)
			if !c.Writer.Written() {
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		}
	}
}

func versionLabel(v string) string {
	if v == "dev" || v == "" {
		return devLabel
	}
	return v
}

func dataOf(v appupdate.View) view.Data {
	d := view.Data{Version: versionLabel(v.Version), CheckedAt: v.CheckedAt, CanCheck: true, Checking: v.State == appupdate.StateChecking}
	d.Status, d.Tone = status(v)
	return d
}

// status is the sentence for the state of the last check and how serious it is.
func status(v appupdate.View) (string, view.Tone) {
	switch v.State {
	case appupdate.StateChecking:
		return "確認しています…", view.ToneNeutral
	case appupdate.StateError:
		return "更新の確認に失敗しました。" + errorText(v.ErrorKind), view.ToneError
	case appupdate.StateReady:
		return fmt.Sprintf("新しいバージョン %s をダウンロードして検証しました。まもなくアプリを再起動して更新します。", v.Latest), view.ToneOK
	case appupdate.StateBlocked:
		return fmt.Sprintf("新しいバージョン %s があります。%s。まもなく再確認します。", v.Latest, blockText(v.BlockedBy)), view.ToneWarn
	case appupdate.StateAvailable:
		return fmt.Sprintf("新しいバージョン %s があります。ダウンロードしています。", v.Latest), view.ToneOK
	case appupdate.StateDevBuild:
		return "開発版のため、更新は確認しません。", view.ToneNeutral
	case appupdate.StateNoRelease:
		return "公開されているリリースはまだありません。", view.ToneNeutral
	case appupdate.StateUpToDate:
		return "最新のバージョンです。", view.ToneOK
	}
	return "まだ確認していません。", view.ToneNeutral
}

func blockText(b appupdate.Block) string {
	switch b {
	case appupdate.BlockJobs:
		return "生成などのバックグラウンド作業が動いているため、終わるまで更新を待っています"
	case appupdate.BlockEditing:
		return "編集の工程が動いているため、終わるまで更新を待っています"
	case appupdate.BlockMedia:
		return "動画の変換（ffmpeg）が動いているため、終わるまで更新を待っています"
	}
	return "作業の実行状況を確認できなかったため、更新を見送っています"
}

func errorText(k appupdate.ErrorKind) string {
	switch k {
	case appupdate.ErrorNetwork:
		return "ネットワークに接続できませんでした。しばらくして再確認します。"
	case appupdate.ErrorRateLimit:
		return "GitHub の取得回数の上限に達しました。しばらくして再確認します。"
	case appupdate.ErrorAccess:
		return "リリースを取得できませんでした（アクセスが拒否されたか、ファイルがありません）。"
	case appupdate.ErrorVerification:
		return "ダウンロードしたファイルを検証できませんでした（破損または改ざんの可能性があるため、更新しません）。"
	case appupdate.ErrorRelease:
		return "リリースの内容が正しくありません。"
	}
	return "原因は不明です。詳しくはログファイルを確認してください。"
}
