package credentials

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	"github.com/ousiassllc/mam/backend/internal/web/handler/failure"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/credentials"
)

const (
	pinnedReason       = "起動オプションで固定されているため、この画面では変更できません"
	providerSaved      = "API プロバイダーを保存しました。次の見積りと生成から、選んだ先で呼びます"
	unreadableProvider = "API プロバイダーの設定を読めません。設定ファイル（models.json）を確認してください"
)

// aggregator says the provider ID is one of the API aggregators: its card is shown while it is the chosen provider. While one is chosen, the cards of the vendors whose
// models it serves (ownVendors) are hidden: their keys are not used then (the saved keys stay).
func aggregator(id string) bool {
	return slices.Contains([]string{"openrouter", "fal", "replicate"}, id)
}

// ownVendors are the keys of the vendors of viaprovider.Models: their own APIs, which an aggregator replaces.
var ownVendors = []string{"kling", "kling4"}

// visible are the cards of the API keys the choice of the provider shows: the official one shows every key of the vendors; an aggregator
// shows its own key first and not the keys of the vendors it stands in for. The rest (LLM, images, the other video models, audio) is always there.
func visible(statuses []service.CredentialStatus, selected modelsettings.Provider) []service.CredentialStatus {
	out := make([]service.CredentialStatus, 0, len(statuses))
	if id := selected.Vendor(); id != "" {
		if i := slices.IndexFunc(statuses, func(s service.CredentialStatus) bool { return s.Provider == id }); i >= 0 {
			out = append(out, statuses[i])
		}
	}
	for _, s := range statuses {
		if aggregator(s.Provider) || (selected != modelsettings.ProviderOfficial && slices.Contains(ownVendors, s.Provider)) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// providerChoice is the choice of the provider as the panel draws it, and the provider chosen (the official one when there is no choice or its file
// cannot be read: the cards are then the usual ones).
func providerChoice(c *gin.Context, k Keys, statuses []service.CredentialStatus) (*view.ProviderData, modelsettings.Provider) {
	if k.Models == nil {
		return nil, modelsettings.ProviderOfficial
	}
	v, err := k.Models.VideoProvider()
	if err != nil {
		_ = c.Error(err)
		return &view.ProviderData{Problem: unreadableProvider}, modelsettings.ProviderOfficial
	}
	keySet := map[string]bool{}
	for _, s := range statuses {
		keySet[s.Provider] = s.Configured
	}
	d := &view.ProviderData{Pinned: v.Pinned}
	for _, p := range modelsettings.Providers() {
		d.Choices = append(d.Choices, view.ProviderChoice{Value: string(p), Label: p.Label(), Checked: p == v.Selected})
	}
	for _, m := range v.Models {
		pm := view.ProviderModel{Label: m.Label, Supported: m.Supported, Why: m.Why, Prices: pricesText(m.PerSecond)}
		if m.Supported && v.Selected != modelsettings.ProviderOfficial {
			pm.KeyLabel, pm.KeySet = v.Selected.Label(), keySet[v.Selected.Vendor()]
		}
		d.Models = append(d.Models, pm)
	}
	return d, v.Selected
}

// pricesText is the unit prices of a model as the card says them: "720p $0.084/秒 ・ 1080p $0.112/秒".
func pricesText(prices []modelsettings.ProviderPrice) string {
	parts := make([]string, 0, len(prices))
	for _, p := range prices {
		parts = append(parts, fmt.Sprintf("%s $%.3f/秒", p.Resolution, p.USD))
	}
	return strings.Join(parts, " ・ ")
}

// SaveProvider stores the choice of the API provider (PUT /settings/api-provider, form provider) and answers with the whole panel, whose
// cards follow the choice: 200 with a notice, 422 with the reason for a provider that does not exist, 409 for a settings file that a start-up
// option fixed. It is applied at once: the next estimate and call go to the chosen provider.
func SaveProvider(k Keys) gin.HandlerFunc {
	return func(c *gin.Context) {
		err := k.Models.SaveVideoProvider(modelsettings.Provider(c.PostForm("provider")))
		status, message := http.StatusOK, ""
		var invalid *modelsettings.InvalidError
		switch {
		case err == nil:
		case errors.Is(err, modelsettings.ErrPinned):
			status, message = http.StatusConflict, pinnedReason
		case errors.As(err, &invalid):
			status, message = http.StatusUnprocessableEntity, invalid.Message
		default:
			_ = c.Error(err)
			status, message = failure.Save(err)
		}
		settingshttp.NoStore(c)
		d := Data(c, k)
		if message != "" {
			d.Error = message
		} else {
			d.Notice = providerSaved
		}
		settingshttp.Render(c, status, view.Panel(d))
	}
}
