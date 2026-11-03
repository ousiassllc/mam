package llm

import (
	"context"
	"net/url"

	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelpicker"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelcard"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/llm"
)

// form is the modal of an Agent drawn from the row held in e (typed: the model names are what the modal's form held, which a refusal draws again; the saved row
// otherwise). Each target's provider and model are one field, the model picker.
func form(ctx context.Context, models *llmmodels.Service, e llmassign.Entry, blame string) view.Form {
	row := rowOf(e)
	return view.Form{
		Row: row, MaxModelLength: llmassign.MaxModelBytes, Blame: blame,
		Primary:  modelField(ctx, models, "primary", row.Primary, row.DefaultPrimary, blame),
		Fallback: modelField(ctx, models, "fallback", row.Fallback, row.DefaultFallback, blame),
	}
}

// modelField is the field of a target: the card of the model it holds (the provider's display name of it when it lists it), whether the provider lists the model at all and
// whether the provider can be used now (#1013).
func modelField(ctx context.Context, models *llmmodels.Service, role string, t, def view.Target, blame string) view.ModelField {
	f := view.ModelField{Role: role, Provider: t.Provider, Selected: t.Model, Default: def}
	name, listed, avail := "", false, modelfilter.Usable()
	if models != nil {
		if list, err := models.List(ctx, t.Provider); err == nil {
			listed = list.Has(t.Model)
			for _, m := range list.Models {
				if m.ID == t.Model {
					name = m.Name
				}
			}
			if avail = list.Availability; !avail.Available {
				f.Unusable, f.Route = avail.Message, avail.Route
			}
		}
	}
	f.Unlisted = t.Model != "" && !listed
	card := modelcard.Card{}
	if t.Model != "" {
		card = modelpicker.LLMCard(t.Provider, t.Model, name, avail)
	}
	f.Field = modelpicker.FieldFor(modelpicker.SourceLLM, view.FieldID(role), "モデル（"+roleWord[role]+"）", url.Values{},
		[]modelpick.Input{{Name: role + ".provider", Key: "provider", Value: t.Provider}, {Name: role + ".model", Key: "model", Value: t.Model}}, card)
	f.Field.Invalid = blame == role+".provider" || blame == role+".model"
	f.Field.Autofocus = f.Field.Invalid || (blame == "" && role == "primary")
	if f.Field.Invalid {
		f.Field.DescribedBy = "mam-modal-error"
	}
	return f
}

var roleWord = map[string]string{"primary": "プライマリ", "fallback": "代替"}
