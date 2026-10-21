package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/credentialpanel"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
)

// GlobalSettingsPage is the settings that belong to no Project (GET /settings): the API keys, and the
// sections that addSections fills in (nil: none). HTMX requests get the sections without the page frame.
func GlobalSettingsPage(keys *service.CredentialService, addSections func(*organisms.GlobalSettingsBodyData)) gin.HandlerFunc {
	return func(c *gin.Context) {
		credentials, err := credentialPanelData(keys)
		if err != nil {
			// Without a usable key store only this panel is lost: the rest of the screen needs no key.
			_ = c.Error(err)
			credentials = credentialpanel.Data{Unavailable: true}
		}
		data := organisms.GlobalSettingsBodyData{Credentials: credentials}
		if addSections != nil {
			addSections(&data)
		}
		if isHTMX(c) {
			render(c, http.StatusOK, organisms.GlobalSettingsBody(data))
			return
		}
		render(c, http.StatusOK, pages.GlobalSettingsPage(middleware.CSRFToken(c), data))
	}
}
