package mmview

import (
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/kind"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// Entry is a model of the catalog as the filter and the screens read it (#1011, #1013): the ModelView and, beside it, the traits of the catalog entry
// (the parameter count, uncensored, the speed and quality guides, the languages), the inputs and the sound that the workflows give, and whether the model can be
// used now (installed). The screens' cards and the MCP tool list_model_catalog both take their Entries from Entries / Where.
type Entry struct {
	modelmanager.ModelView
	// Traits are the fields of the entry's "traits" object. Uncensored is not the content rating: an uncensored model is always an adult one, but not every
	// adult model is uncensored.
	Traits traits.Traits `json:"traits"`
	// Inputs are what the model takes as input (t2i, i2v, first_frame, reference_face...), derived from its workflows; Audio: a video with sound.
	Inputs []traits.Input `json:"inputs"`
	Audio  bool           `json:"audio"`
	// Availability is whether the model can be used now: only an installed model can; the others say why and the model manager is the route that fixes it.
	modelfilter.Availability
}

// Entries lists the catalog models (modelmanager.Service.Catalog: an adult model only while the age gate is open) as Entries.
func Entries(s *modelmanager.Service) []Entry {
	views := s.Catalog()
	out := make([]Entry, 0, len(views))
	for _, v := range views {
		out = append(out, EntryOf(s, v))
	}
	return out
}

// Where lists the Entries that pass the condition, in their order.
func Where(s *modelmanager.Service, c modelfilter.Condition) []Entry {
	return modelfilter.Filter(c, Entries(s), Entry.Item)
}

// EntryOf is the Entry of one model view (modelmanager.Service.Model, Catalog).
func EntryOf(s *modelmanager.Service, v modelmanager.ModelView) Entry {
	e := Entry{ModelView: v, Inputs: []traits.Input{}, Availability: availability(v.State)}
	if m, ok := s.Deps.Catalog.Model(v.ID); ok {
		d := m.Derived()
		e.Traits, e.Audio = m.Traits, d.Audio
		if d.Inputs != nil {
			e.Inputs = d.Inputs
		}
	}
	return e
}

// availability is whether a model in this state can be used: only an installed one (#1013).
func availability(st modelmanager.ModelState) modelfilter.Availability {
	var msg string
	switch st {
	case modelmanager.ModelInstalled:
		return modelfilter.Usable()
	case modelmanager.ModelInstalling:
		msg = "導入中です。終わるまでお待ちください"
	case modelmanager.ModelIncomplete:
		msg = "ファイルが足りません。モデルマネージャーで入れ直してください"
	default:
		msg = "ローカルに導入されていません（モデルマネージャーで導入します）"
	}
	return modelfilter.Unusable(modelfilter.ReasonNotInstalled, msg, modelfilter.RouteOf(modelfilter.ReasonNotInstalled, modelfilter.SourceLocal))
}

// Item is the model as the filter reads it (modelfilter.Condition.Match).
func (e Entry) Item() modelfilter.Item {
	it := modelfilter.Item{
		ID: e.ID, Name: e.Name, Inputs: e.Inputs, Adult: e.ContentRating == catalog.RatingAdult, Uncensored: e.Traits.Uncensored, ParamsB: e.Traits.ParamsB,
		VRAMMinGB: e.VRAM.MinGB, Commercial: e.License.Commercial, Installed: e.State == modelmanager.ModelInstalled, Origin: modelfilter.OriginLocal, Provider: "local",
		Audio: e.Audio, Speed: e.Traits.Speed, Quality: e.Traits.Quality, Tags: append([]string{string(e.Kind), string(e.Tier)}, e.TriggerWords...),
		Available: e.Available, Reason: e.Message,
	}
	switch e.Kind {
	case localcfg.MediaVideo:
		it.Kinds = []modelfilter.Kind{modelfilter.KindVideo}
	case kind.Upscale:
		it.Kinds = []modelfilter.Kind{modelfilter.KindUpscale}
	default:
		it.Kinds = []modelfilter.Kind{modelfilter.KindImage}
		if slices.Contains(e.Inputs, traits.Edit) || slices.Contains(e.Inputs, traits.Inpaint) {
			it.Kinds = append(it.Kinds, modelfilter.KindImageEdit)
		}
	}
	return it
}

// InfoOf is what the catalog says of the model an install created, by its identifier (comfyui:bundled-comfyui/<workflow name>); false for any other model.
// The generation test reads it to filter its list by the same adult flag and traits as the model manager (gentest.Deps.CatalogInfo).
func InfoOf(s *modelmanager.Service) func(modelID string) (modelfilter.CatalogInfo, bool) {
	return func(modelID string) (modelfilter.CatalogInfo, bool) {
		k, endpoint, name, ok := localcfg.ParseID(modelID)
		if !ok || k != localcfg.KindComfyUI || endpoint != modelmanager.BundledEndpoint {
			return modelfilter.CatalogInfo{}, false
		}
		for _, m := range s.Deps.Catalog.Models {
			if slices.ContainsFunc(m.Workflows, func(w catalog.Workflow) bool { return w.Name == name }) {
				return modelfilter.CatalogInfo{Adult: m.Adult(), Traits: m.Traits, VRAMMinGB: m.VRAM.MinGB}, true
			}
		}
		return modelfilter.CatalogInfo{}, false
	}
}
