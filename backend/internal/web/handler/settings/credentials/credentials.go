// Package credentials holds the handlers of the API keys of the settings screen (spec/components/settings-screen-categories.md
// §1.1–1.2): the data of the cards, the modal that sets a key, the save and the delete, and the connection test. It is a
// package of its own because handler is at its size limit; like handler it depends on the service layer and the web layer
// only. No handler reads a stored key into an answer: a key goes from the form to the key store (or the provider, in a
// test) and the answers say only whether one is set.
package credentials

import (
	"errors"
	"net/http"
	"slices"
	"strconv"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/credential/probe"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modal"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/credentials"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Keys is what the handlers of the API keys work on: the key service, and the connection tests (nil: the screen has none).
type Keys struct {
	Svc    *service.CredentialService
	Tester *probe.Tester
}

const (
	saveFailed   = "API キーを保存できませんでした。OS のキーチェーン（Secret Service など）を確認してください"
	deleteFailed = "API キーを削除できませんでした。OS のキーチェーン（Secret Service など）を確認してください"
	notFound     = "対象が見つかりません"
)

// providerViews copies the key status of the service into the cards (what the key is for, whether it can be tested).
func providerViews(k Keys, statuses []service.CredentialStatus) []view.ProviderView {
	views := make([]view.ProviderView, 0, len(statuses))
	for _, s := range statuses {
		views = append(views, providerView(k, s))
	}
	return views
}

func providerView(k Keys, s service.CredentialStatus) view.ProviderView {
	v := view.ProviderView{Provider: s.Provider, Label: s.Label, Configured: s.Configured, CanTest: k.Tester.CanTestConnection(s.Provider)}
	providerInfo[s.Provider].apply(&v)
	return v
}

// Data is the API keys as the settings screen shows them. A key store that cannot be read leaves the cards without a
// state: only this panel is lost, and the error is attached to the request for the log.
func Data(c *gin.Context, k Keys) view.Data {
	statuses, err := k.Svc.Status()
	if err != nil {
		_ = c.Error(err)
		return view.Data{Unavailable: true, Providers: providerViews(k, unknownStatuses())}
	}
	return view.Data{Providers: providerViews(k, statuses)}
}

// unknownStatuses are the providers without a state (the key store cannot be read): every card is "未設定" and cannot be edited.
func unknownStatuses() []service.CredentialStatus {
	out := make([]service.CredentialStatus, 0, len(service.CredentialProviders))
	for _, p := range service.CredentialProviders {
		out = append(out, service.CredentialStatus{Provider: p.ID, Label: p.Label})
	}
	return out
}

// Edit answers the modal that sets the key of :provider (GET /settings/credentials/:provider/edit). An unknown provider is
// 404 with a modal that says so. A request that is not HTMX (the address typed in, a reload) gets page, the category page.
func Edit(k Keys, page gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !settingshttp.IsHTMX(c) {
			page(c)
			return
		}
		settingshttp.NoStore(c)
		v, unavailable, ok := find(c, k, c.Param("provider"))
		if !ok {
			settingshttp.Render(c, http.StatusNotFound, modal.Notice("API キー", notFound))
			return
		}
		settingshttp.Render(c, http.StatusOK, view.Modal(view.Form{Provider: v, Unavailable: unavailable}, service.MaxAPIKeyBytes))
	}
}

// find is the card of provider as the service sees it now. unavailable: the key store cannot be read, so the card has no state.
func find(c *gin.Context, k Keys, provider string) (v view.ProviderView, unavailable, ok bool) {
	statuses, err := k.Svc.Status()
	if err != nil {
		_ = c.Error(err)
		i := slices.IndexFunc(service.CredentialProviders, func(p struct{ ID, Label string }) bool { return p.ID == provider })
		if i < 0 {
			return view.ProviderView{}, false, false
		}
		p := service.CredentialProviders[i]
		return providerView(k, service.CredentialStatus{Provider: p.ID, Label: p.Label}), true, true
	}
	i := slices.IndexFunc(statuses, func(s service.CredentialStatus) bool { return s.Provider == provider })
	if i < 0 {
		return view.ProviderView{}, false, false
	}
	return providerView(k, statuses[i]), false, true
}

// Save stores the API key of a provider (PUT /credentials/:provider, form api_key) for the modal that sent it. The answer
// is a card replaced out of band, a toast, and the event that closes the modal; the key is not echoed back. A refused key
// is 422 and a key store that refused is 503: the modal again, with why, and the stored key stays. A key store that cannot
// be read afterwards leaves no card to draw: the whole panel is replaced by the "unavailable" one, with a notice (a saved
// key is not reported lost).
func Save(k Keys) gin.HandlerFunc {
	return func(c *gin.Context) {
		provider := c.Param("provider")
		err := k.Svc.Save(provider, c.PostForm("api_key"))
		if invalid := asValidation(err); invalid != nil && invalid.Field == "provider" {
			toast(c, http.StatusUnprocessableEntity, invalid.Message)
			return
		}
		done(c, k, provider, err, "API キーを保存しました", "API キーを保存しました。キーストアの状態は確認できませんでした", saveFailed)
	}
}

// Delete removes the API key of a provider (DELETE /credentials/:provider) and answers with the card of the provider. Deleting
// a key that is not set is not an error.
func Delete(k Keys) gin.HandlerFunc {
	return func(c *gin.Context) {
		provider := c.Param("provider")
		err := k.Svc.Remove(provider)
		if invalid := asValidation(err); invalid != nil {
			toast(c, http.StatusUnprocessableEntity, invalid.Message)
			return
		}
		if err != nil {
			_ = c.Error(err)
			toast(c, http.StatusServiceUnavailable, deleteFailed)
			return
		}
		settingshttp.NoStore(c)
		statuses, statusErr := k.Svc.Status()
		if statusErr != nil {
			unreadable(c, k, http.StatusOK, "API キーを削除しました。キーストアの状態は確認できませんでした", statusErr)
			return
		}
		i := slices.IndexFunc(statuses, func(s service.CredentialStatus) bool { return s.Provider == provider })
		settingshttp.Render(c, http.StatusOK, templ.Join(view.Row(providerView(k, statuses[i])), atoms.SuccessToastOOB("API キーを削除しました")))
	}
}

// done answers a save of the modal: see Save.
func done(c *gin.Context, k Keys, provider string, opErr error, saved, savedUnreadable, failed string) {
	settingshttp.NoStore(c)
	if invalid := asValidation(opErr); invalid != nil {
		refuse(c, k, provider, http.StatusUnprocessableEntity, invalid.Message)
		return
	}
	if opErr != nil {
		_ = c.Error(opErr)
		refuse(c, k, provider, http.StatusServiceUnavailable, failed)
		return
	}
	statuses, statusErr := k.Svc.Status()
	if statusErr != nil {
		settingshttp.CloseModal(c)
		unreadable(c, k, http.StatusOK, savedUnreadable, statusErr)
		return
	}
	settingshttp.CloseModal(c)
	i := slices.IndexFunc(statuses, func(s service.CredentialStatus) bool { return s.Provider == provider })
	settingshttp.Render(c, http.StatusOK, templ.Join(view.RowOOB(providerView(k, statuses[i])), atoms.SuccessToastOOB(saved)))
}

// refuse answers a save that did not happen with the modal again and why: the key field is empty, the reason is told.
func refuse(c *gin.Context, k Keys, provider string, status int, message string) {
	v, unavailable, ok := find(c, k, provider)
	if !ok {
		settingshttp.Render(c, http.StatusNotFound, modal.Notice("API キー", notFound))
		return
	}
	settingshttp.Render(c, status, view.Modal(view.Form{Provider: v, Error: message, Unavailable: unavailable}, service.MaxAPIKeyBytes))
}

// unreadable answers with the whole panel when the key store cannot be read after the operation: there is no state to
// draw a card from. notice is what worked.
func unreadable(c *gin.Context, k Keys, status int, notice string, statusErr error) {
	_ = c.Error(statusErr)
	c.Header("HX-Retarget", "#credential-panel")
	c.Header("HX-Reswap", "outerHTML")
	settingshttp.Render(c, status, view.Panel(view.Data{Unavailable: true, Notice: notice, Providers: providerViews(k, unknownStatuses())}))
}

// toast answers with a toast and leaves the page alone.
func toast(c *gin.Context, status int, message string) {
	middleware.ToastOnly(c)
	settingshttp.Render(c, status, atoms.ToastOOB(message))
}

func asValidation(err error) *service.ValidationError {
	var v *service.ValidationError
	if errors.As(err, &v) {
		return v
	}
	return nil
}

// Test tries a key against its provider without saving it (POST /settings/credentials/:provider/test, form api_key; empty
// means the saved key) and answers with the line of the modal that says what the provider answered. A provider that
// refuses the key, cannot be reached or limits the calls is an answer, not a failure: 200. The key is in no answer.
func Test(tester *probe.Tester) gin.HandlerFunc {
	return func(c *gin.Context) {
		settingshttp.NoStore(c)
		res, err := tester.TestConnection(c.Request.Context(), c.Param("provider"), c.PostForm("api_key"))
		var input *probe.InputError
		switch {
		case errors.Is(err, probe.ErrUnsupported):
			settingshttp.Render(c, http.StatusNotFound, view.TestResult(atoms.BadgeNeutral, "このプロバイダーには接続テストがありません"))
		case errors.As(err, &input):
			settingshttp.Render(c, http.StatusUnprocessableEntity, view.TestResult(atoms.BadgeWarn, input.Message))
		case err != nil:
			_ = c.Error(err)
			settingshttp.Render(c, http.StatusServiceUnavailable, view.TestResult(atoms.BadgeWarn, "保存済みのキーを読めません。キーストアを確認してください"))
		default:
			tone, text := describe(res)
			settingshttp.Render(c, http.StatusOK, view.TestResult(tone, text))
		}
	}
}

// describe is what a test result says.
func describe(r probe.Result) (atoms.BadgeTone, string) {
	switch r.Outcome {
	case probe.Accepted:
		return atoms.BadgeOK, "接続できました。キーは受け付けられました"
	case probe.Rejected:
		return atoms.BadgeError, "キーが拒否されました。キーを確かめてください"
	case probe.RateLimited:
		return atoms.BadgeWarn, "上限に達しています。しばらくしてからもう一度試してください"
	case probe.Unreachable:
		return atoms.BadgeWarn, "通信できませんでした。ネットワークを確認してください"
	}
	return atoms.BadgeWarn, "想定外の応答でした（HTTP " + strconv.Itoa(r.Status) + "）。キーが使えるかは分かりません"
}

// Register adds the routes of the API keys: the modal (GET paths.CredentialEdit), the connection test (POST
// paths.CredentialTest), and the save and the delete (PUT / DELETE paths.Credential). page answers the modal route for a
// request that is not HTMX.
func Register(engine *gin.Engine, k Keys, page gin.HandlerFunc) {
	engine.GET(paths.CredentialEdit(":provider"), Edit(k, page))
	engine.POST(paths.CredentialTest(":provider"), Test(k.Tester))
	engine.PUT(paths.Credential(":provider"), Save(k))
	engine.DELETE(paths.Credential(":provider"), Delete(k))
}
