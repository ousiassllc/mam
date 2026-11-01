package chatgptsettings

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex"
	"github.com/ousiassllc/mam/backend/internal/exeinstall"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/chatgpt"
)

// kindCodex is the Codex login system in the paths (":kind" of the login and logout routes).
const kindCodex = "codex"

// codexData reads the Codex login for the section. It starts nothing: the account is the one last read.
func codexData(svc *codex.Service) view.Codex {
	st := svc.Status()
	c := view.Codex{
		SignedIn: st.Account.SignedIn, Email: st.Account.Email, Plan: st.Account.PlanType, CredentialsStore: st.CredentialsStore,
		Engine: view.CodexEngine{
			State: string(st.Engine), Supported: st.Supported, DownloadBytes: st.DownloadBytes, ExtractedBytes: st.ExtractedBytes, Source: string(st.Source), Version: st.Version,
			PinnedVersion: st.PinnedVersion, UpdateAvailable: st.UpdateAvailable, CustomPath: st.CustomPath,
			Error: st.EngineError, Dir: st.Dir, License: st.License.Name,
			Install: view.CodexInstall{
				Running: st.Install.State == codex.InstallRunning, Percent: st.Install.Percent, Message: st.Install.Message,
			},
		},
	}
	if st.Install.State == codex.InstallFailed {
		c.Engine.Install.Error = st.Install.Error
	}
	for _, w := range st.RateLimits {
		c.Limits = append(c.Limits, view.CodexLimit{Name: w.Name, UsedPercent: w.UsedPercent, ResetsAt: w.ResetsAt})
	}
	if l, ok := svc.CurrentLogin(); ok {
		switch {
		case l.State == codex.LoginPending || (l.State != codex.LoginSucceeded && time.Since(l.FinishedAt) < finishedLoginShown):
			c.Login = &view.Login{
				Pending: l.State == codex.LoginPending, Failed: l.State == codex.LoginFailed, Message: l.Message,
				AuthURL: l.AuthURL, BrowserOpened: l.BrowserOpened, VerificationURL: l.VerificationURL, UserCode: l.UserCode,
				ExpiresAt: l.ExpiresAt, Device: l.Method == codex.MethodDevice,
			}
		case c.SignedIn && time.Since(l.FinishedAt) < finishedLoginShown:
			c.JustSignedIn = true
		}
	}
	return c
}

// codexSection is the section after an action on the Codex login.
func codexSection(svc *chatgpt.Service) view.Data { return SectionData(svc) }

// CodexModels lists the models of the signed-in Codex account in a modal (GET /settings/chatgpt/codex/models, model/list).
func CodexModels(svc *chatgpt.Service, page gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !settingshttp.IsHTMX(c) {
			page(c)
			return
		}
		c.Header("Cache-Control", "no-store")
		list := view.ModelList{Kind: kindCodex}
		models, err := svc.Codex().Models(c.Request.Context())
		if err != nil {
			_ = c.Error(err)
			list.Error = "モデルの一覧を取得できませんでした。ログインの状態を確認して、もう一度お試しください"
		}
		for _, m := range models {
			list.Models = append(list.Models, view.Model{Slug: m.Slug, DisplayName: m.DisplayName, Images: m.Images})
		}
		render(c, http.StatusOK, view.ModelsModal(list))
	}
}

// CodexRefresh reads the account and the usage limits again (POST /settings/chatgpt/codex/refresh). It starts the
// engine when needed.
func CodexRefresh(svc *chatgpt.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, err := svc.Codex().Refresh(c.Request.Context())
		d := codexSection(svc)
		status := http.StatusOK
		if err != nil {
			_ = c.Error(err)
			status, d.Error = http.StatusBadGateway, "Codex の状態を読めませんでした。エンジンが取得済みか、エンジンのエラーを確認してください"
		} else {
			d.Notice = "状態を更新しました"
		}
		render(c, status, view.Panel(d))
	}
}

// CodexInstall starts the download of the pinned engine (POST /settings/chatgpt/codex/install).
func CodexInstall(svc *chatgpt.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		started, err := svc.Codex().StartInstall()
		d := codexSection(svc)
		status := http.StatusOK
		var noSpace *fetch.NoSpaceError
		switch {
		case err == nil && started:
			d.Notice = "ダウンロードを始めました"
		case err == nil:
			d.Notice = "すでにダウンロード中です"
		case errors.Is(err, codex.ErrUnsupported):
			status, d.Error = http.StatusConflict, "この環境ではダウンロードできません（ダウンロードできるのは Windows 版と macOS 版（Apple Silicon）だけです）。自分で入れた codex のパスを指定してください"
		case errors.Is(err, exeinstall.ErrNotWritable):
			status, d.Error = http.StatusConflict, fmt.Sprintf("Codex の保存先（%s）に書き込めません。書き込み権限を確認してください（Windows 版は mam の実行ファイルの置き場所、macOS 版はデータディレクトリの中です）", svc.Codex().Status().Dir)
		case errors.As(err, &noSpace):
			status, d.Error = http.StatusInsufficientStorage, fmt.Sprintf("ディスクの空きが足りません（必要 約 %d MB、空き %d MB）", noSpace.Need>>20, noSpace.Free>>20)
		default:
			_ = c.Error(err)
			status, d.Error = http.StatusInternalServerError, "ダウンロードを始められませんでした。mam のログを確認してください"
		}
		render(c, status, view.Panel(d))
	}
}

// CodexInstallCancel stops the download (POST /settings/chatgpt/codex/install/cancel); installing again resumes it.
func CodexInstallCancel(svc *chatgpt.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc.Codex().CancelInstall()
		svc.Codex().WaitInstall()
		d := codexSection(svc)
		d.Notice = "ダウンロードを中断しました。もう一度押すと続きから取得します"
		render(c, http.StatusOK, view.Panel(d))
	}
}

// CodexPathResult is what a save of the codex path did, in the words the settings screen and the first-run guide share.
type CodexPathResult struct {
	Status  int
	Message string
}

// OK is whether the path was saved (or cleared).
func (r CodexPathResult) OK() bool { return r.Status == http.StatusOK }

// SetCodexPath sets the codex the user installed, or clears it with "" (chatgpt.Service.Codex().SetCustomPath). A path that
// is not an absolute path of a file is the user's mistake (422); a path that could not be saved is not: 500, and the error
// goes to the log through c.
func SetCodexPath(c *gin.Context, svc *chatgpt.Service, path string) CodexPathResult {
	err := svc.Codex().SetCustomPath(path)
	switch {
	case err == nil && path == "":
		return CodexPathResult{http.StatusOK, "パスの指定を解除しました"}
	case err == nil:
		return CodexPathResult{http.StatusOK, "パスを設定しました。mam は実行して確かめません。ログインやモデルの取得で使われます"}
	case errors.Is(err, codex.ErrInvalidPath):
		return CodexPathResult{http.StatusUnprocessableEntity, "パスを設定できませんでした。実在するファイルの絶対パスを指定してください"}
	}
	_ = c.Error(err)
	return CodexPathResult{http.StatusInternalServerError, "パスを保存できませんでした。データフォルダの権限と空き容量を確認してください"}
}

// CodexSetPath sets the codex executable the user installed, or clears it (PUT /settings/chatgpt/codex/path, field path).
func CodexSetPath(svc *chatgpt.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		r := SetCodexPath(c, svc, c.PostForm("path"))
		d := codexSection(svc)
		if r.OK() {
			d.Notice = r.Message
		} else {
			d.Error = r.Message
		}
		render(c, r.Status, view.Panel(d))
	}
}

// startCodexLogin starts a browser or device-code login of the Codex system.
func startCodexLogin(c *gin.Context, svc *chatgpt.Service) {
	method := codex.MethodBrowser
	if c.PostForm("method") == "device" {
		method = codex.MethodDevice
	}
	_, err := svc.Codex().StartLogin(c.Request.Context(), method)
	status, message := http.StatusOK, ""
	var ls *codex.LoginStartError
	switch {
	case err == nil:
	case errors.Is(err, codex.ErrEngineNotInstalled):
		status, message = http.StatusConflict, "先に Codex をダウンロードするか、自分で入れた codex のパスを指定してください"
	case errors.Is(err, codex.ErrEngineInstalling):
		status, message = http.StatusConflict, "Codex のエンジンを導入中です。終わってからもう一度試してください"
	case errors.As(err, &ls):
		_ = c.Error(err)
		status, message = http.StatusBadGateway, ls.Message
	default:
		_ = c.Error(err)
		status, message = http.StatusInternalServerError, "Codex を起動できませんでした。エンジンのエラーと mam のログを確認してください"
	}
	loginAnswer(c, codexSection(svc), kindCodex, status, message)
}

// cancelCodexLogin stops the Codex login in progress.
func cancelCodexLogin(c *gin.Context, svc *chatgpt.Service) {
	if l, ok := svc.Codex().CurrentLogin(); ok {
		svc.Codex().CancelLogin(c.Request.Context(), l.ID)
	}
	cancelled(c, svc)
}

// logoutCodex signs out of Codex; the field remove=true also deletes the downloaded engine and mam's CODEX_HOME.
func logoutCodex(c *gin.Context, svc *chatgpt.Service) {
	res, err := svc.Codex().Logout(c.Request.Context(), c.Request.FormValue("remove") == "true")
	d := codexSection(svc)
	status := http.StatusOK
	switch {
	case err != nil:
		_ = c.Error(err)
		status, d.Error = http.StatusInternalServerError, "ログアウトを完了できませんでした（保存したトークンは消えていません）。エンジンのエラーと mam のログを確認してください"
	case res.CredentialsMayRemain:
		d.Notice = "ファイルを削除しました。ただし、エンジンが無かったため、OS の資格情報ストアに Codex の資格情報が残っている可能性があります。Windows の「資格情報マネージャー」で「Codex」を探して削除してください"
	case res.Removed:
		d.Notice = "ログアウトして、取得した Codex 本体と mam 専用の CODEX_HOME を削除しました"
	default:
		d.Notice = "ログアウトしました"
	}
	render(c, status, view.Panel(d))
}
