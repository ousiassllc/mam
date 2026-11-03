// Package licenseview turns the license of a catalog model into what the model manager screen shows. It is a package of its own because the
// directory of the handlers is at its line budget (linterly).
package licenseview

import (
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
)

var regionNames = map[string]string{"EU": "EU", "GB": "英国", "KR": "韓国"}

// Of is the license of a catalog model as the screen shows it: the commercial-use answer in words and the regions that are excluded.
func Of(l catalog.License) view.License {
	out := view.License{ID: l.ID, Version: l.Version, Name: l.Name, URL: l.URL, Summary: l.Summary, Notice: l.Notice, Answer: commercial.FromInfo(l.Info())}
	switch {
	case l.Commercial == modellicense.Conditional && l.CommercialCondition != "":
		out.Commercial = "条件つきで可（" + l.CommercialCondition + "）"
	case l.Commercial == modellicense.Conditional:
		out.Commercial = "条件つきで可"
	case l.Commercial == modellicense.Yes && l.CommercialCondition != "":
		out.Commercial = "可（" + l.CommercialCondition + "）"
	case l.Commercial == modellicense.Yes:
		out.Commercial = "可"
	case l.Commercial == modellicense.Unknown:
		out.Commercial = "不明（確認できていません）"
	case l.CommercialCondition != "":
		out.Commercial = "不可（" + l.CommercialCondition + "）"
	default:
		out.Commercial = "不可（非商用）"
	}
	if len(l.ExcludedRegions) > 0 {
		names := make([]string, len(l.ExcludedRegions))
		for i, r := range l.ExcludedRegions {
			names[i] = regionNames[r]
			if names[i] == "" {
				names[i] = r
			}
		}
		out.Regions = "次の地域では、このライセンスは適用されず、このモデルを使えません: " + strings.Join(names, "・")
	}
	return out
}
