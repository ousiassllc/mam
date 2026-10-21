package handler

import (
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/credentialpanel"
)

// credentialProviderViews copies the service's key status into the rows the panel shows.
func credentialProviderViews(statuses []service.CredentialStatus) []credentialpanel.ProviderView {
	views := make([]credentialpanel.ProviderView, 0, len(statuses))
	for _, s := range statuses {
		views = append(views, credentialpanel.ProviderView{Provider: s.Provider, Label: s.Label, Configured: s.Configured})
	}
	return views
}

func credentialPanelData(svc *service.CredentialService) (credentialpanel.Data, error) {
	statuses, err := svc.Status()
	return credentialpanel.Data{Providers: credentialProviderViews(statuses)}, err
}

// credentialOutcome answers a key save or delete with the row of the provider (the keys typed into the
// other rows are kept): 200; 422 with the reason when the key was refused (the stored key stays); 503 with
// failed when the key store refused. A key store that cannot be read leaves no row to draw: the whole panel
// is replaced by the "unavailable" one, with notice when the operation worked (a saved key is not reported lost).
func credentialOutcome(c *gin.Context, svc *service.CredentialService, provider string, opErr error, notice, failed string) {
	code, message := http.StatusOK, ""
	if invalid := asValidation(opErr); invalid != nil {
		code, message = http.StatusUnprocessableEntity, invalid.Message
	} else if opErr != nil {
		_ = c.Error(opErr)
		code, message = http.StatusServiceUnavailable, failed
	}
	data, statusErr := credentialPanelData(svc)
	if statusErr != nil {
		_ = c.Error(statusErr)
		data = credentialpanel.Data{Unavailable: true}
		if opErr == nil {
			data.Notice = notice
		}
		c.Header("HX-Retarget", "#credential-panel")
		c.Header("HX-Reswap", "outerHTML")
		render(c, code, credentialpanel.Panel(data))
		return
	}
	i := slices.IndexFunc(data.Providers, func(p credentialpanel.ProviderView) bool { return p.Provider == provider })
	if i < 0 { // a provider that is not managed has no row to answer with
		toast(c, opErr)
		return
	}
	render(c, code, credentialpanel.Row(data.Providers[i], message))
}

// SaveCredential stores the API key of a provider (PUT /credentials/:provider, form api_key) and
// answers with the row of the provider. The key is not echoed back.
func SaveCredential(svc *service.CredentialService) gin.HandlerFunc {
	return func(c *gin.Context) {
		provider := c.Param("provider")
		err := svc.Save(provider, c.PostForm("api_key"))
		credentialOutcome(c, svc, provider, err, "API キーを保存しました。キーストアの状態は確認できませんでした",
			"API キーを保存できませんでした。OS のキーチェーン（Secret Service など）を確認してください")
	}
}

// DeleteCredential removes the API key of a provider (DELETE /credentials/:provider) and answers
// with the row of the provider. Deleting a key that is not set is not an error.
func DeleteCredential(svc *service.CredentialService) gin.HandlerFunc {
	return func(c *gin.Context) {
		provider := c.Param("provider")
		err := svc.Remove(provider)
		if asValidation(err) != nil {
			toast(c, err)
			return
		}
		credentialOutcome(c, svc, provider, err, "API キーを削除しました。キーストアの状態は確認できませんでした",
			"API キーを削除できませんでした。OS のキーチェーン（Secret Service など）を確認してください")
	}
}
