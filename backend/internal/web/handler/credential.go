package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
)

func credentialPanelData(svc *service.CredentialService) (organisms.CredentialPanelData, error) {
	providers, err := svc.Status()
	return organisms.CredentialPanelData{Providers: providers}, err
}

// SaveCredential stores the API key of a provider (PUT /credentials/:provider, form api_key) and
// answers with the panel. The key is not echoed back. A refused key re-renders the panel with the
// reason (422) and keeps the stored key.
func SaveCredential(svc *service.CredentialService) gin.HandlerFunc {
	return func(c *gin.Context) {
		provider := c.Param("provider")
		err := svc.Save(provider, c.PostForm("api_key"))
		data, derr := credentialPanelData(svc)
		if derr != nil {
			toast(c, derr)
			return
		}
		if err == nil {
			render(c, http.StatusOK, organisms.CredentialPanel(data))
			return
		}
		if invalid := asValidation(err); invalid != nil {
			data.Error, data.ErrorProvider = invalid.Message, provider
			render(c, http.StatusUnprocessableEntity, organisms.CredentialPanel(data))
			return
		}
		toast(c, err)
	}
}

// DeleteCredential removes the API key of a provider (DELETE /credentials/:provider) and answers
// with the panel. Deleting a key that is not set is not an error.
func DeleteCredential(svc *service.CredentialService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := svc.Remove(c.Param("provider")); err != nil {
			toast(c, err)
			return
		}
		data, err := credentialPanelData(svc)
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, organisms.CredentialPanel(data))
	}
}
