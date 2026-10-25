// Package chatgptsettings holds the handlers of the "ChatGPT でログイン" section of the global settings screen
// (spec/architecture/chatgpt-signin.md). It is a package of its own because handler is at its size limit; like handler,
// it depends on the service layer and the web layer only. No handler reads a token: the services give facts about a sign-in.
package chatgptsettings

import (
	"errors"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modal"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/chatgpt"
)

// The kind names of the paths. The image login system is stored as chatgpt.KindImage.
const (
	kindOfficial = string(chatgpt.KindOfficial)
	kindImage    = string(chatgpt.KindImage)
)

// finishedLoginShown is how long the result of a finished login stays on the panel.
const finishedLoginShown = 5 * time.Minute

// SectionData is the section as the settings screen shows it. A sign-in that cannot be read (the OS storage does not
// answer) is shown as not signed in, with the reason as the error.
func SectionData(svc *chatgpt.Service) view.Data {
	d := view.Data{Risks: chatgpt.ImageRisks}
	ov, err := svc.Overview()
	if err != nil {
		d.Error = "ログインの状態を読み込めませんでした。OS のキーストアを確認してください"
	}
	d.Official = accountData(svc.Official(), ov.Official)
	d.Image = accountData(svc.Image(), ov.Image)
	d.ImageEnabled = ov.ImageEnabled
	d.Codex = codexData(svc.Codex())
	return d
}

func accountData(a *chatgpt.Account, s chatgpt.Status) view.Account {
	out := view.Account{
		SignedIn: s.SignedIn, NeedsRelogin: s.NeedsRelogin, Email: s.Email, Plan: s.Plan, ExpiresAt: s.ExpiresAt,
		PlanUsageGranted: s.PlanUsageGranted, WorkspacePlan: s.WorkspacePlan,
	}
	if l, ok := a.CurrentLogin(); ok {
		if l.State == chatgpt.LoginPending || (l.State != chatgpt.LoginSucceeded && time.Since(l.FinishedAt) < finishedLoginShown) {
			out.Login = &view.Login{
				Pending: l.State == chatgpt.LoginPending, Failed: l.State == chatgpt.LoginFailed, Message: l.Message,
				AuthURL: l.AuthURL, BrowserOpened: l.BrowserOpened, VerificationURL: l.VerificationURL, UserCode: l.UserCode,
				ExpiresAt: l.ExpiresAt, Device: l.Method == chatgpt.MethodDevice, UseCodex: l.UseCodex,
			}
		}
	}
	return out
}

// Panel answers the cards alone (GET /settings/chatgpt). The open login and the download of the engine poll it. A request
// that is not HTMX (the address typed in, a reload) gets page: the "API キーとログイン" category, of which this is a part.
func Panel(svc *chatgpt.Service, page gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !settingshttp.IsHTMX(c) {
			page(c)
			return
		}
		c.Header("Cache-Control", "no-store")
		render(c, http.StatusOK, view.Panel(SectionData(svc)))
	}
}

// Models lists the models of the signed-in plan in a modal (GET /settings/chatgpt/models; read only).
func Models(svc *chatgpt.Service, page gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !settingshttp.IsHTMX(c) {
			page(c)
			return
		}
		c.Header("Cache-Control", "no-store")
		list := view.ModelList{Kind: kindOfficial}
		models, err := svc.Models(c.Request.Context())
		if err != nil {
			if !errors.Is(err, chatgpt.ErrNotSignedIn) && !errors.Is(err, chatgpt.ErrReloginRequired) && !errors.Is(err, chatgpt.ErrPlanUsageNotGranted) {
				_ = c.Error(err)
			}
			list.Error = "モデルの一覧を取得できませんでした。ログインの状態を確認して、もう一度お試しください"
		}
		for _, m := range models {
			list.Models = append(list.Models, view.Model{Slug: m.Slug, DisplayName: m.DisplayName})
		}
		render(c, http.StatusOK, view.ModelsModal(list))
	}
}

// loginOf is the login of a system in the data: the one in progress or the one that just failed.
func loginOf(d view.Data, kind string) *view.Login {
	switch kind {
	case kindImage:
		return d.Image.Login
	case kindCodex:
		return d.Codex.Login
	}
	return d.Official.Login
}

func signedIn(d view.Data, kind string) bool {
	switch kind {
	case kindImage:
		return d.Image.SignedIn
	case kindCodex:
		return d.Codex.SignedIn
	}
	return d.Official.SignedIn
}

// LoginModal answers the modal of a login (GET /settings/chatgpt/:kind/login): the choice of the method, or what to do while
// the login is pending. With poll=1 (the pending modal asking again) a login that has ended is the answer too: signed in
// closes the modal and replaces the cards; a failure is shown in the modal. A request that is not HTMX gets page, whatever
// the kind; an unknown kind asked by HTMX gets a 404 modal that says so.
func LoginModal(svc *chatgpt.Service, page gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !settingshttp.IsHTMX(c) {
			page(c)
			return
		}
		kind := c.Param("kind")
		if !validKind(kind) {
			render(c, http.StatusNotFound, modal.Notice("ChatGPT でログイン", "対象が見つかりません"))
			return
		}
		c.Header("Cache-Control", "no-store")
		d := SectionData(svc)
		login := loginOf(d, kind)
		if c.Query("poll") == "1" && (login == nil || !login.Pending) {
			endedLogin(c, d, kind, login)
			return
		}
		render(c, http.StatusOK, view.LoginModal(view.LoginForm{Kind: kind, Login: login}))
	}
}

// endedLogin answers a poll of a login that is no longer pending: closed with the cards replaced when it worked, the failure
// in the modal when it did not, the form again when it was cancelled.
func endedLogin(c *gin.Context, d view.Data, kind string, login *view.Login) {
	d.OOB = true
	if login != nil && login.Failed {
		render(c, http.StatusOK, templ.Join(view.LoginModal(view.LoginForm{Kind: kind, Login: login}), view.Panel(d)))
		return
	}
	if signedIn(d, kind) {
		settingshttp.CloseModal(c)
		render(c, http.StatusOK, view.Panel(d))
		return
	}
	render(c, http.StatusOK, templ.Join(view.LoginModal(view.LoginForm{Kind: kind}), view.Panel(d)))
}

// loginAnswer is the answer to the request of a login (the modal body) with the cards replaced out of band, so that they
// say "ログイン待ち" at once. status and message are why the login did not start.
func loginAnswer(c *gin.Context, d view.Data, kind string, status int, message string) {
	d.OOB = true
	form := view.LoginForm{Kind: kind, Login: loginOf(d, kind), Error: message}
	render(c, status, templ.Join(view.LoginModal(form), view.Panel(d)))
}

func account(svc *chatgpt.Service, kind string) *chatgpt.Account {
	if kind == kindImage {
		return svc.Image()
	}
	return svc.Official()
}

func validKind(kind string) bool {
	return kind == kindOfficial || kind == kindImage || kind == kindCodex
}

// officialSection is the section after an action on the official or the image login.
func officialSection(svc *chatgpt.Service) view.Data { return SectionData(svc) }

// StartLogin starts a login (POST /settings/chatgpt/:kind/login, field method: browser or device) and answers with the
// modal showing it (and the cards). The image login refuses while the image generation is off; the official one has no
// device login.
func StartLogin(svc *chatgpt.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		kind := c.Param("kind")
		if !validKind(kind) {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		if kind == kindCodex {
			startCodexLogin(c, svc)
			return
		}
		var err error
		switch {
		case kind == kindImage && c.PostForm("method") == "device":
			_, err = svc.StartImageDeviceLogin(c.Request.Context())
		case kind == kindImage:
			_, err = svc.StartImageBrowserLogin()
		case c.PostForm("method") == "device":
			err = chatgpt.ErrNoDeviceLogin
		default:
			_, err = svc.Official().StartBrowserLogin()
		}
		status, message := http.StatusOK, ""
		switch {
		case err == nil:
		case errors.Is(err, chatgpt.ErrImageDisabled):
			status, message = http.StatusConflict, "画像生成（非公式）がオフです。先にオンにしてください"
		case errors.Is(err, chatgpt.ErrNoDeviceLogin):
			status, message = http.StatusUnprocessableEntity, "公式のログインにはデバイスコードの方式がありません。mam が動いているこの PC のブラウザでログインしてください"
		default:
			_ = c.Error(err)
			status, message = http.StatusInternalServerError, "ログインを始められませんでした。ポート 1455 / 1457 が他のアプリに使われていないか、mam のログを確認してください"
		}
		loginAnswer(c, SectionData(svc), kind, status, message)
	}
}

// CancelLogin stops the login in progress (POST /settings/chatgpt/:kind/login/cancel) and answers by closing the modal and
// replacing the cards, which say so.
func CancelLogin(svc *chatgpt.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		kind := c.Param("kind")
		if !validKind(kind) {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		if kind == kindCodex {
			cancelCodexLogin(c, svc)
			return
		}
		a := account(svc, kind)
		if l, ok := a.CurrentLogin(); ok {
			a.CancelLogin(l.ID)
		}
		cancelled(c, svc)
	}
}

// cancelled answers a cancelled login: the modal closes and the cards say that it was stopped.
func cancelled(c *gin.Context, svc *chatgpt.Service) {
	d := SectionData(svc)
	d.OOB, d.Notice = true, "ログインを中止しました"
	settingshttp.CloseModal(c)
	render(c, http.StatusOK, view.Panel(d))
}

// Logout revokes the refresh token and deletes the local tokens (DELETE /settings/chatgpt/:kind). When revocation could
// not be confirmed, the answer says so.
func Logout(svc *chatgpt.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		kind := c.Param("kind")
		if !validKind(kind) {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		if kind == kindCodex {
			logoutCodex(c, svc)
			return
		}
		res, err := account(svc, kind).Logout(c.Request.Context())
		d := officialSection(svc)
		status := http.StatusOK
		switch {
		case err != nil:
			_ = c.Error(err)
			status, d.Error = http.StatusInternalServerError, "ログアウトを完了できませんでした。OS のキーストアと mam のログを確認してください"
		case res.WasSignedIn && !res.RevokeConfirmed:
			d.Notice = "ログアウトしました。ただし、OpenAI 側での失効を確認できませんでした。念のため ChatGPT の設定（Settings）で mam の接続を解除してください"
		default:
			d.Notice = "ログアウトしました"
		}
		render(c, status, view.Panel(d))
	}
}

// SetImageEnabled turns the unofficial image generation on or off (PUT /settings/chatgpt/image/enabled, fields enabled
// and acknowledge_risks). Turning it on without the confirmation is refused with 422.
func SetImageEnabled(svc *chatgpt.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		enabled := c.PostForm("enabled") == "true"
		err := svc.SetImageEnabled(enabled, c.PostForm("acknowledge_risks") == "true")
		d := SectionData(svc)
		status := http.StatusOK
		switch {
		case err == nil && enabled:
			d.Notice = "画像生成（非公式）をオンにしました。続けて、下のログインを済ませてください"
		case err == nil:
			d.Notice = "画像生成（非公式）をオフにしました。この経路は呼びません"
		case errors.Is(err, chatgpt.ErrRisksNotAcknowledged):
			status, d.Error = http.StatusUnprocessableEntity, "リスクを確認するにチェックを入れてから、オンにしてください"
		default:
			_ = c.Error(err)
			status, d.Error = http.StatusInternalServerError, "設定を保存できませんでした。mam のログを確認してください"
		}
		render(c, status, view.Panel(d))
	}
}

func render(c *gin.Context, status int, component templ.Component) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	if err := component.Render(c.Request.Context(), c.Writer); err != nil {
		_ = c.Error(err)
		if !c.Writer.Written() {
			c.AbortWithStatus(http.StatusInternalServerError)
		}
	}
}
