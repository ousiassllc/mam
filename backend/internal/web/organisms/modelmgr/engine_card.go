package modelmgr

import (
	"github.com/a-h/templ"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelcard"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// EngineName is the name of the engine card (the title of its modal and the subject of its buttons).
const EngineName = "ComfyUI エンジン"

// EnginePhase is where the engine is, in one word (the data-engine of the card): unsupported, readonly, installing, broken, missing or installed.
func EnginePhase(e Engine) string {
	switch {
	case !e.Supported:
		return "unsupported"
	case !e.Writable:
		return "readonly"
	case e.Install != nil:
		return "installing"
	case e.Broken:
		return "broken"
	case !e.Installed:
		return "missing"
	}
	return "installed"
}

var phaseBadges = map[string]struct {
	tone  atoms.BadgeTone
	label string
}{
	"unsupported": {atoms.BadgeNeutral, "この PC では使えません"}, "readonly": {atoms.BadgeError, "導入先に書き込めません"},
	"installing": {atoms.BadgeInfo, "導入中"}, "broken": {atoms.BadgeWarn, "ファイルが見つかりません"}, "missing": {atoms.BadgeNeutral, "未導入"},
}

// EngineBadge is the one badge that sums the card up (also the one of its modal): the phase, and once installed the state of the process.
func EngineBadge(e Engine) (atoms.BadgeTone, string) {
	if b, ok := phaseBadges[EnginePhase(e)]; ok {
		return b.tone, b.label
	}
	return stateBadge(e.State)
}

// engineCard is what the card says (modelcard.Card): the version and build under the name once installed. A card that polls carries the request
// on its <li>, as the runtimes do; the one that opens the modal is its name.
func engineCard(e Engine) modelcard.Card {
	tone, label := EngineBadge(e)
	phase, meta := EnginePhase(e), "画像・動画のモデルを動かすエンジン"
	attrs := templ.Attributes{"data-testid": "mm-engine", "data-engine": phase, "data-state": e.State}
	if e.Polling() {
		attrs["hx-get"], attrs["hx-trigger"], attrs["hx-swap"] = engineURL(e), Polling, "outerHTML"
	}
	c := modelcard.Card{ID: EngineID, Name: EngineName, Icon: atoms.IconWorkflow, DetailURL: paths.ModelManagerPart("engine", "detail"), Attrs: attrs,
		State: modelcard.Badge{Tone: tone, Label: label, Attrs: templ.Attributes{"data-testid": "mm-engine-state", "data-state": e.State}}}
	if phase == "installed" {
		meta = "ComfyUI " + e.Version + "・" + e.VariantLabel
		if e.CPU {
			meta += "・CPU 実行"
		}
		if e.UpdateAvailable {
			c.Badges = append(c.Badges, modelcard.Badge{Tone: atoms.BadgeInfo, Label: "更新あり"})
		}
	}
	c.Meta = meta
	return c
}
