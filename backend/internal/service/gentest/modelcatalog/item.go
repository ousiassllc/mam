package modelcatalog

import (
	"cmp"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/agegate"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
)

// CatalogTraits is what the model manager's catalog says of a model it installed (Info.Catalog).
type CatalogTraits struct {
	Adult     bool          `json:"adult" jsonschema:"成人向け（content_rating が adult）"`
	Traits    traits.Traits `json:"traits" jsonschema:"パラメータ数・非検閲・速度と品質の目安（カタログの traits）"`
	VRAMMinGB int           `json:"vram_min_gb" jsonschema:"VRAM の目安（最小。GB）"`
	// MissingOptional are the optional files that are not in place (the model is installed and usable without them).
	MissingOptional []string `json:"missing_optional_files,omitempty" jsonschema:"導入済みで、まだ無い任意のファイル（速さのモードの Lightning LoRA など）。モデルは使えるが、それを使う速さのモードは使えない"`
	PartialNote     string   `json:"partial_note,omitempty" jsonschema:"任意のファイルが足りないことを説明する文（導入済み・速さのモードの一部は未導入）"`
}

func (c Catalog) catalogInfo(id string) (modelfilter.CatalogInfo, bool) {
	if c.CatalogInfo == nil {
		return modelfilter.CatalogInfo{}, false
	}
	return c.CatalogInfo(id)
}

// originOf is "local" for a model on this PC or a registered endpoint and "api" for a vendor's API and the ChatGPT subscription.
func originOf(source string) string {
	if source == SourceLocal || source == SourceWorkflow {
		return string(modelfilter.OriginLocal)
	}
	return string(modelfilter.OriginAPI)
}

// providerOf is the provider of the model as the filter names it.
func providerOf(m domain.ModelName, source string) string {
	switch source {
	case SourceLocal, SourceWorkflow:
		return "local"
	case SourceChatGPT:
		return string(modelfilter.ProviderChatGPT)
	}
	return string(modeladapter.VendorOf(m))
}

// inputsOf derives the inputs of the model from what Info already says of its references and its video: the facts package traits reads out of a
// workflow, here read out of the capabilities, so the filter and the capabilities agree.
func inputsOf(i Info) []traits.Input {
	f := traits.Facts{ReferenceMode: i.ReferenceMode, MaxRefs: i.MaxReferenceImages, Roles: i.ReferenceRoles}
	f.NeedsReference = i.ReferenceMode == string(localcfg.ModeControlNet) || i.ReferenceMode == string(localcfg.ModeIPAdapter)
	if i.AcceptsReferences && f.MaxRefs == 0 {
		f.MaxRefs = 1 // a cloud model that takes references and does not say how many
	}
	if i.Video != nil {
		f = traits.Facts{
			Video: true, MaxRefs: i.Video.MaxReferenceImages, FirstFrame: i.Video.FirstFrame, LastFrame: i.Video.LastFrame, NativeAudio: i.Video.NativeAudio,
			FrameOptional: originOf(i.Source) == string(modelfilter.OriginAPI),
		}
	} else if i.Media == "video" {
		f.Video = true
	}
	in := f.Inputs()
	if in == nil {
		in = []traits.Input{}
	}
	return in
}

// Item is the model as the filter reads it (modelfilter.Condition.Match).
func (i Info) Item() modelfilter.Item {
	it := modelfilter.Item{
		ID: i.ID, Name: i.Label, Inputs: i.Inputs, Commercial: i.License.Commercial, Installed: true, Origin: modelfilter.Origin(i.Origin), Provider: i.Provider,
		Audio: i.Video != nil && i.Video.NativeAudio, Tags: []string{i.Source, i.Media}, Available: i.Available, Reason: i.UnavailableMessage,
	}
	if i.Media == "video" {
		it.Kinds = []modelfilter.Kind{modelfilter.KindVideo}
	} else {
		it.Kinds = []modelfilter.Kind{modelfilter.KindImage}
		if hasInput(i.Inputs, traits.Edit) || hasInput(i.Inputs, traits.Inpaint) {
			it.Kinds = append(it.Kinds, modelfilter.KindImageEdit)
		}
	}
	if c := i.Catalog; c != nil {
		it.Adult, it.Uncensored, it.ParamsB, it.VRAMMinGB, it.Speed, it.Quality = c.Adult, c.Traits.Uncensored, c.Traits.ParamsB, c.VRAMMinGB, c.Traits.Speed, c.Traits.Quality
	}
	return it
}

func hasInput(in []traits.Input, want traits.Input) bool {
	for _, i := range in {
		if i == want {
			return true
		}
	}
	return false
}

// Availability is whether the model can be chosen now, in the shape every picker and the MCP tools share (#1013).
func (i Info) Availability() modelfilter.Availability {
	if i.Available {
		return modelfilter.Usable()
	}
	return modelfilter.Unusable(modelfilter.Reason(i.Unavailable), i.UnavailableMessage, i.SettingsRoute)
}

// Filter keeps the models the condition lets through, in their order.
func Filter(models []Info, c modelfilter.Condition) []Info {
	return modelfilter.Filter(c, models, Info.Item)
}

// Availability is whether one model can be chosen now, with the reason and the screen that fixes it (#1013): the answer of Info.Availability without describing
// the model (its price and capabilities are not read). False for a model that does not exist.
func (c Catalog) Availability(id string) (modelfilter.Availability, bool) {
	cfg := c.Adapter.Config()
	for _, list := range [][]domain.ModelName{domain.ImageModels(), domain.VideoModels()} {
		for _, m := range list {
			if string(m) == id && !c.hidden(m) {
				var info *modelfilter.CatalogInfo
				if i, ok := c.catalogInfo(id); ok {
					info = &i
				}
				return c.availability(cfg, m, sourceOf(cfg, m), info), true
			}
		}
	}
	return modelfilter.Availability{}, false
}

// availability is the one judgement of "can this model be chosen now": the adapter's reason (no backend or API key, the vendor's breaker, the age gate), else the
// model manager's own state of a catalog model (not installed, being installed, a required file missing: the same answer as the catalog's, #1025), else a custom
// node of the workflow that is missing. info is what the catalog said of the model (nil: it knows none); the caller asks it once, for it stats the model's files.
func (c Catalog) availability(cfg modeladapter.Config, m domain.ModelName, source string, info *modelfilter.CatalogInfo) modelfilter.Availability {
	reason, msg := c.Adapter.Unavailable(m), ""
	if reason != "" {
		msg = cmp.Or(providerMessage(cfg, m, reason), unavailableMessage(reason, source))
	} else if info != nil && info.NotInstalled != "" {
		reason, msg = string(modelfilter.ReasonNotInstalled), info.NotInstalled
	} else if msg = c.missingNodes(cfg, m); msg != "" {
		reason = ExcludedMissingNode
	} else {
		return modelfilter.Usable()
	}
	code := modelfilter.Reason(reason)
	if reason == agegate.Excluded { // the adapter's reason is a sentence; the list says the code
		code = modelfilter.ReasonAgeGate
	}
	return modelfilter.Unusable(code, msg, modelfilter.RouteOf(code, source))
}

// Item is the upscale workflow as the filter sees it (an upscale model on this PC: local, installed).
func (u Upscaler) Item() modelfilter.Item {
	return modelfilter.Item{
		ID: u.ID, Name: u.Label, Kinds: []modelfilter.Kind{modelfilter.KindUpscale}, Commercial: u.License.Commercial, Installed: true,
		Origin: modelfilter.OriginLocal, Provider: string(modelfilter.OriginLocal), Tags: []string{"upscale", u.Mode}, Available: u.Available, Reason: u.UnavailableMessage,
	}
}

// Availability is whether the upscale workflow can be chosen now, in the shape every picker shares.
func (u Upscaler) Availability() modelfilter.Availability {
	if u.Available {
		return modelfilter.Usable()
	}
	return modelfilter.Unusable(modelfilter.Reason(u.Unavailable), u.UnavailableMessage, "")
}
