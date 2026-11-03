package gentest

import (
	"math"
	"os"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/maskcheck"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

// WarnDenoiseIgnored is the warning when a model has no denoise assignment, so the redraw strength is not sent.
const WarnDenoiseIgnored = "denoise_ignored"

// checkInpaint refuses a partial redraw the models cannot do, and a denoise out of range. A request with neither a mask nor a
// denoise is not a partial redraw, so it is refused when a model can only do partial redraws (SupportsInpaint).
func checkInpaint(r Request, models []modelcatalog.Info) error {
	if r.Mask == nil && r.Denoise == nil {
		if slices.ContainsFunc(models, func(m modelcatalog.Info) bool { return m.SupportsInpaint }) {
			return gterr.Invalid("一部だけ描き直す専用のモデルです。mask を指定してください")
		}
		return nil
	}
	if r.Media != MediaImage {
		return gterr.Invalid("一部だけ描き直すのは画像だけです")
	}
	if r.Mask == nil {
		return gterr.Invalid("一部だけ描き直すにはマスク画像が要ります")
	}
	if err := checkSource(*r.Mask); err != nil {
		return err
	}
	if len(r.References) != 1 {
		return gterr.Invalid("一部だけ描き直すには元画像を 1 枚だけ指定してください")
	}
	if r.Denoise != nil && !denoiseOK(*r.Denoise) {
		return gterr.Invalid("描き直しの強さは 0 より大きく 1 以下にしてください")
	}
	for _, m := range models {
		if !m.SupportsInpaint {
			return gterr.Invalid("%s は一部だけ描き直すに対応していないため選べません", m.Label)
		}
	}
	return nil
}

func denoiseOK(v float64) bool { return v > 0 && v <= 1 && !math.IsNaN(v) }

func maskPaintsFile(path string) error {
	data, err := os.ReadFile(path) //nolint:gosec // path comes from ResolveInput, under the gentest or project directory
	if err != nil {
		return gterr.Invalid("マスク画像のファイルが見つかりません")
	}
	return maskcheck.Paints(data)
}
