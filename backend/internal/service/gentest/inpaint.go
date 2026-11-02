package gentest

import (
	"bytes"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"

	_ "golang.org/x/image/webp"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

// WarnDenoiseIgnored is the warning when a model has no denoise assignment, so the redraw strength is not sent.
const WarnDenoiseIgnored = "denoise_ignored"

// checkInpaint refuses a partial redraw the models cannot do, and a denoise out of range. A request with neither a mask nor a
// denoise is not a partial redraw.
func checkInpaint(r Request, models []modelcatalog.Info) error {
	if r.Mask == nil && r.Denoise == nil {
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

// maskPaints reports whether the mask has a near-white pixel (the part that is redrawn).
func maskPaints(data []byte) error {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return gterr.Invalid("マスクは PNG・JPEG・WebP の画像にしてください")
	}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r > 0xC000 && g > 0xC000 && bl > 0xC000 {
				return nil
			}
		}
	}
	return gterr.Invalid("マスクが真っ黒です。描き直す部分を白く塗ってください")
}

func maskPaintsFile(path string) error {
	data, err := os.ReadFile(path) //nolint:gosec // path comes from ResolveInput, under the gentest or project directory
	if err != nil {
		return gterr.Invalid("マスク画像のファイルが見つかりません")
	}
	return maskPaints(data)
}
