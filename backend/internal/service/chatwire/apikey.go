package chatwire

import (
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/apikey"
	_ "github.com/ousiassllc/mam/backend/internal/service/chatops/outofscope" // registers the refusal of what is GUI-only on purpose
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

// The chat operations of the API keys and of the API provider of the cloud video models (service/chatops/apikey, #1140): the same CredentialService and
// modelsettings.Service the settings screen and the MCP tools use.
func init() {
	builders = append(builders, func(env Env, reg *chatops.Runtime) {
		creds := env.Credentials
		reg.Handle(apikey.OpSetCredential, apikey.NewKey(apikey.KeyDeps{
			Configured: creds.Configured,
			Save:       func(provider, key string) error { return refusalOf(creds.Save(provider, key)) },
		}))
		models := env.Settings.Models
		if models == nil {
			return
		}
		reg.Handle(apikey.OpSetVideoProvider, apikey.NewProvider(apikey.ProviderDeps{
			Configured: creds.Configured,
			View: func() (apikey.ProviderView, error) {
				v, err := models.VideoProvider()
				return apikey.ProviderView{Selected: string(v.Selected), Pinned: v.Pinned}, err
			},
			Save: func(provider string) error {
				return refusalOf(models.SaveVideoProvider(modelsettings.Provider(provider)))
			},
		}))
	})
}
