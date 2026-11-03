package media

import (
	"context"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelcards"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelpicker"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelcard"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/media"
)

// Cards says what a model is, for the fields of the modals (the model picker, #1011): the generation test's description of the model, which is the card every list of
// models shows. Without the service (a router built without it) a field shows the model's name and its commercial-use answer.
type Cards struct{ Gentest *gentest.Service }

// card is the card of the model of the media that a field shows as chosen; a model the generation test does not know is a card with its name.
func (k Cards) card(ctx context.Context, mediaName, id, label string) modelcard.Card {
	if k.Gentest != nil {
		if info, err := k.Gentest.Model(ctx, mediaName, id); err == nil {
			c := modelcards.FromInfo(info)
			if label != "" {
				c.Name = label
			}
			return c
		}
	}
	answer := commercial.Of(id)
	return modelcard.Card{Name: cardName(id, label), Icon: iconOf(mediaName), Commercial: &answer}
}

func iconOf(mediaName string) atoms.IconName {
	if mediaName == gentest.MediaVideo {
		return atoms.IconVideo
	}
	return atoms.IconImage
}

func cardName(id, label string) string {
	if label != "" {
		return label
	}
	return id
}

// field is the field of one slot: its hidden input (named as the whole assignment's form names it, so that one parser reads both), what it shows, and the refusal's mark.
func field(source, name, label, value string, current modelcard.Card, unusable, blame, first bool) modelpick.FieldData {
	f := modelpicker.FieldFor(source, name, label, nil, []modelpick.Input{{Name: name, Key: "value", Value: value}}, current)
	f.Autofocus = first || blame
	if blame {
		f.Invalid, f.DescribedBy = true, "mam-modal-error"
	}
	if unusable {
		f.Note = value + media.UnusableSuffix
	}
	return f
}

// imageForm is the modal of image purpose p for the route held in r (the saved one, or what was typed).
func (k Cards) imageForm(ctx context.Context, v modelsettings.View, p modelsettings.ImagePurpose, r modelsettings.ImageRoute, msg, blame string) media.ImageForm {
	row := imageRow(v, p, r)
	slot := func(s string, t media.ImageTarget, first bool) modelpick.FieldData {
		name := media.ImageModelField(row.Purpose, s)
		return field(modelpicker.SourceSettingsImage, name, slotLabel(s), t.Model, k.card(ctx, gentest.MediaImage, t.Model, ""), t.Unusable, blame == s, first)
	}
	return media.ImageForm{
		Row: row, Models: imageModels(v), Primary: slot(media.SlotPrimary, row.Primary, msg == ""), Fallback: slot(media.SlotFallback, row.Fallback, false), Error: msg, Blame: blame,
	}
}

// policyForm is the modal of Quality Policy q for the route held in r.
func (k Cards) policyForm(ctx context.Context, v modelsettings.View, q domain.QualityPolicy, r modelsettings.PolicyRoute, msg, blame string) media.PolicyForm {
	row := policyRow(v, q, r)
	slot := func(s, value string, unusable, first bool) modelpick.FieldData {
		name := media.PolicyTargetField(row.Policy, s)
		model, resolution, _ := strings.Cut(value, "@")
		current := k.card(ctx, gentest.MediaVideo, model, "")
		if resolution != "" {
			current.Name += "（" + resolution + "）"
		}
		return field(modelpicker.SourceSettingsVideo, name, slotLabel(s), value, current, unusable, blame == s, first)
	}
	return media.PolicyForm{
		Row: row, Primary: slot(media.SlotPrimary, row.Primary, row.PrimaryUnusable, msg == ""), Fallback: slot(media.SlotFallback, row.Fallback, row.FallbackUnusable, false),
		Error: msg, Blame: blame,
	}
}

func slotLabel(slot string) string {
	if slot == media.SlotPrimary {
		return "主モデル"
	}
	return "代替モデル"
}
