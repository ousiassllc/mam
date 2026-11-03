// Package catalogfilter is the filter of the model list of the model manager screen (#1011). It is a package of its own because the directory of the
// handlers is at its line budget (linterly).
package catalogfilter

import (
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
	"github.com/ousiassllc/mam/backend/internal/web/handler/pickfilter"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// BarID is the id of the filter bar of the model list; the list is view.CatalogID.
const BarID = "mm-model-bar"

// Screen is the filter of the model list (#1011): the screen starts at the zero condition (an uninstalled model is shown too; "導入済みのみ" is a choice of the panel),
// and remembers what was chosen under "model-manager". The panel offers what tells the catalog's models apart; every model is local, so there is no 提供元 and no
// "選べるものだけ" (the installed filter says it).
func Screen() pickfilter.Screen {
	return pickfilter.Screen{
		Key: "model-manager", BarID: BarID, Action: paths.ModelManagerPart("models"), Target: "#" + view.CatalogID, Start: modelfilter.Condition{},
		Facets: modelpick.Facets{
			Kinds:  []modelfilter.Kind{modelfilter.KindImage, modelfilter.KindVideo, modelfilter.KindImageEdit, modelfilter.KindUpscale},
			Inputs: []traits.Input{traits.T2I, traits.I2I, traits.Edit, traits.Inpaint, traits.T2V, traits.I2V, traits.FirstFrame, traits.LastFrame, traits.ReferenceWhole, traits.ReferenceFace, traits.ReferenceStyle, traits.ReferencePose},
			Adult:  true, Uncensored: true, Params: true, VRAM: true, Commercial: true, Installed: true, Audio: true, Speed: true, Quality: true,
		},
	}
}
