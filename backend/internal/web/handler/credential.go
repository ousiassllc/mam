package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
)

// credentialProviderViews copies the service's key status into the rows the panel shows.
func credentialProviderViews(statuses []service.CredentialStatus) []organisms.CredentialProviderView {
	views := make([]organisms.CredentialProviderView, 0, len(statuses))
	for _, s := range statuses {
		views = append(views, organisms.CredentialProviderView{Provider: s.Provider, Label: s.Label, Configured: s.Configured})
	}
	return views
}

func credentialPanelData(svc *service.CredentialService) (organisms.CredentialPanelData, error) {
	statuses, err := svc.Status()
	return organisms.CredentialPanelData{Providers: credentialProviderViews(statuses)}, err
}

// credentialOutcome answers a key save or delete with the panel, whatever the key store does:
//   - the operation worked: 200. If the key store cannot be read afterwards, the panel is the
//     "unavailable" one and carries notice, so that a key that was saved is not reported as lost;
//   - the key was refused: 422 with the reason, the stored key stays;
//   - the key store refused the operation: 503 with failed (and the "unavailable" panel when it cannot
//     be read at all, which tells to check the OS key chain).
//
// The panel is never replaced by a generic internal error: the key store is the one part that is
// not under our control.
func credentialOutcome(c *gin.Context, svc *service.CredentialService, provider string, opErr error, notice, failed string) {
	data, statusErr := credentialPanelData(svc)
	if statusErr != nil {
		_ = c.Error(statusErr)
		data = organisms.CredentialPanelData{Unavailable: true}
	}
	if opErr == nil {
		if statusErr != nil {
			data.Notice = notice
		}
		render(c, http.StatusOK, organisms.CredentialPanel(data))
		return
	}
	if invalid := asValidation(opErr); invalid != nil {
		data.Error, data.ErrorProvider = invalid.Message, provider
		render(c, http.StatusUnprocessableEntity, organisms.CredentialPanel(data))
		return
	}
	_ = c.Error(opErr)
	data.Error, data.ErrorProvider = failed, provider
	render(c, http.StatusServiceUnavailable, organisms.CredentialPanel(data))
}

// SaveCredential stores the API key of a provider (PUT /credentials/:provider, form api_key) and
// answers with the panel. The key is not echoed back. A refused key re-renders the panel with the
// reason (422) and keeps the stored key; a key store that cannot save answers 503.
func SaveCredential(svc *service.CredentialService) gin.HandlerFunc {
	return func(c *gin.Context) {
		provider := c.Param("provider")
		err := svc.Save(provider, c.PostForm("api_key"))
		credentialOutcome(c, svc, provider, err, "API キーを保存しました。キーストアの状態は確認できませんでした",
			"API キーを保存できませんでした。OS のキーチェーン（Secret Service など）を確認してください")
	}
}

// DeleteCredential removes the API key of a provider (DELETE /credentials/:provider) and answers
// with the panel. Deleting a key that is not set is not an error; a provider that is not managed
// is refused with a toast (422) and a key store that cannot delete answers 503.
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
