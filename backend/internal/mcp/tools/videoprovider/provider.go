// Package videoprovider holds the MCP tools of the API provider of the cloud video models (Kling): the choice at the top of the API
// key category of the settings screen (spec/architecture/model-adapters.md §9.6). It is a package of its own because tools/settings is at its
// size limit; it works on the same service as the screen (modelsettings.Service), so both read and write models.json alike.
package videoprovider

import (
	"context"
	"errors"
	"net/http"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/viaprovider"
	"github.com/ousiassllc/mam/backend/internal/apperr/errmap"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

// Register adds the tools of this package. The model settings come from r.Settings.Models; without them the tools answer "unavailable".
func Register(r *mcpkit.Registrar, svc *service.Services) {
	h := handlers{r: r, svc: svc}
	mcpkit.Read(r, "get_video_provider",
		"クラウド動画モデル（Kling）を呼ぶ API プロバイダーの設定（画面の「API キー」の先頭の選択）。選択肢（official・openrouter・fal・replicate）ごとに必要な API キー（key_provider と key_set。値は返さない）、選んだプロバイダーが各モデルを提供するか（supported）、提供しないときの理由（why。別のプロバイダーか公式で使える）、その単価（prices。見積り・費用はこの単価で出る）を返す。キーは list_credentials・set_credential / The API provider the cloud video models are called through, with the key each needs, whether it serves each model (and why not) and its unit prices.",
		h.getProvider)
	mcpkit.Write(r, "set_video_provider",
		"クラウド動画モデル（Kling）を呼ぶ API プロバイダーを保存する（official・openrouter・fal・replicate。既定は official）。すぐに反映され、次の見積りと生成から選んだ先の単価・提供状況で動く。選んだ先に API キーがなければ、そのモデルは使えない扱いになる（set_credential で登録する）。--model-config で固定されているときは conflict / Save the API provider of the cloud video models (effective at once). The provider's own API key is needed (set_credential).",
		h.setProvider, mcpkit.Idempotent())
}

type handlers struct {
	r   *mcpkit.Registrar
	svc *service.Services
}

// Input has no field.
type Input struct{}

const pinnedMessage = "起動オプションで固定されているため、この画面では変更できません"

func unavailable(what string) error {
	return &mcpkit.ToolError{Code: mcpkit.CodeUnavailable, Message: what + " はこのアプリでは使えません", Status: http.StatusServiceUnavailable}
}

// saveError turns the refusals of the model settings into the errors the tools map: an invalid input is a validation error, a pinned file a
// conflict, anything else the common error of a failed save.
func saveError(err error) error {
	var invalid *modelsettings.InvalidError
	switch {
	case errors.As(err, &invalid):
		return &service.ValidationError{Message: invalid.Message}
	case errors.Is(err, modelsettings.ErrPinned):
		return &service.ConflictError{Message: pinnedMessage}
	}
	return errmap.SaveFailure(err)
}

// Choice is one provider that can be chosen.
type Choice struct {
	Provider string `json:"provider" jsonschema:"set_video_provider に渡す値: official / openrouter / fal / replicate"`
	Label    string `json:"label"`
	Selected bool   `json:"selected"`
	KeyID    string `json:"key_provider,omitempty" jsonschema:"このプロバイダーに必要な API キーの provider 名（set_credential に渡す）。official は空（モデルごとのベンダーのキー）"`
	KeySet   bool   `json:"key_set" jsonschema:"その API キーが設定済みか（値は返さない）。official は常に false（各ベンダーのキーは list_credentials）"`
}

// Price is the price of a second of video at a resolution through the chosen provider (USD).
type Price struct {
	Resolution string  `json:"resolution"`
	USD        float64 `json:"usd_per_second"`
}

// Model is how the chosen provider serves one cloud video model.
type Model struct {
	Model     string  `json:"model" jsonschema:"動画モデル名（kling-3.0・kling-4.0）"`
	Label     string  `json:"label"`
	Supported bool    `json:"supported" jsonschema:"選んだプロバイダーがこのモデルを提供するか"`
	Why       string  `json:"why,omitempty" jsonschema:"supported が false のとき、使えない理由と、代わりに使えるプロバイダー（画面と同じ文）"`
	Prices    []Price `json:"prices,omitempty" jsonschema:"選んだプロバイダーの単価（見積り・費用と同じ値）。supported が false のときは空"`
}

// Section is the API provider of the cloud video models (the top of the API key category).
type Section struct {
	Available bool     `json:"available" jsonschema:"false ならこのアプリには画像・動画モデルの設定がない"`
	Pinned    bool     `json:"pinned" jsonschema:"起動オプション（--model-config）で固定されていて、変更できない"`
	Provider  string   `json:"provider" jsonschema:"選んでいるプロバイダー（既定は official）"`
	Providers []Choice `json:"providers"`
	Models    []Model  `json:"models"`
}

func (h handlers) providerSection() (Section, error) {
	svc := h.r.Settings.Models
	if svc == nil {
		return Section{}, unavailable("API プロバイダーの設定")
	}
	v, err := svc.VideoProvider()
	if err != nil {
		return Section{}, saveError(err)
	}
	out := Section{Available: true, Pinned: v.Pinned, Provider: string(v.Selected), Providers: []Choice{}, Models: []Model{}}
	for _, p := range viaprovider.Providers() {
		c := Choice{Provider: string(p), Label: p.Label(), Selected: p == v.Selected, KeyID: p.Vendor()}
		if c.KeyID != "" && h.svc != nil {
			if c.KeySet, err = h.svc.Credentials.Configured(c.KeyID); err != nil {
				return Section{}, err
			}
		}
		out.Providers = append(out.Providers, c)
	}
	for _, m := range v.Models {
		pm := Model{Model: string(m.Model), Label: m.Label, Supported: m.Supported, Why: m.Why}
		for _, p := range m.PerSecond {
			pm.Prices = append(pm.Prices, Price{Resolution: p.Resolution, USD: p.USD})
		}
		out.Models = append(out.Models, pm)
	}
	return out, nil
}

func (h handlers) getProvider(_ context.Context, _ Input) (Section, error) {
	return h.providerSection()
}

// SetProviderInput is the provider to use.
type SetProviderInput struct {
	Provider string `json:"provider" jsonschema:"クラウド動画モデル（Kling）を呼ぶ API プロバイダー: official（公式。既定）・openrouter・fal・replicate"`
}

func (h handlers) setProvider(_ context.Context, in SetProviderInput) (Section, error) {
	svc := h.r.Settings.Models
	if svc == nil {
		return Section{}, unavailable("API プロバイダーの設定")
	}
	if err := svc.SaveVideoProvider(viaprovider.Provider(in.Provider)); err != nil {
		return Section{}, saveError(err)
	}
	return h.providerSection()
}
