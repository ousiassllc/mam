package gentest

import (
	"os"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/gptimage"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/markupcheck"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

// refusedBy is the reason a model cannot take what (the words of the request: "複雑さ・GIF・SVG の指定" or "マークアップ"): its own refusal (the
// ChatGPT-subscription path, an unofficial endpoint) or the plain sentence. Settings that a model cannot take are never dropped: the request is refused.
func refusedBy(m modelcatalog.Info, what string) error {
	if m.GPTImageRefusal != "" {
		return gterr.Invalid("%s は%sを受け付けません: %s", m.Label, what, m.GPTImageRefusal)
	}
	return gterr.Invalid("%s は%sに対応していません（GPT Image 2.6 の API キー経由のモデルだけ）。対応するモデルだけを選ぶか、指定を外してください", m.Label, what)
}

// checkImageSettings is the check of the settings that only some image models take: GPT Image 2.6's, then the partial redraw's.
func checkImageSettings(r Request, models []modelcatalog.Info) error {
	if err := checkGPTImage(r, models); err != nil {
		return err
	}
	return checkInpaint(r, models)
}

// markupIndex is the index of the markup reference of the request (-1: none).
func markupIndex(r Request) int {
	return slices.IndexFunc(r.References, func(ref RefInput) bool { return ref.Role == gentestrepo.RoleMarkup })
}

// checkGPTImage refuses what GPT Image 2.6's settings and markup image cannot do, without looking at the files (checkMarkupFiles does): a model that cannot
// take them is named (every chosen model must take them: nothing is dropped, nothing is replaced by another model), the values are checked, and the markup
// needs one edit target, no inpaint mask and no SVG output (the edits endpoint has none).
func checkGPTImage(r Request, models []modelcatalog.Info) error {
	markup := markupIndex(r)
	if r.GPTImage.Zero() && markup < 0 {
		return nil
	}
	if r.Media != gentestrepo.MediaImage {
		return gterr.Invalid("複雑さ・GIF・SVG・マークアップは画像だけで使えます")
	}
	if !r.GPTImage.Zero() {
		for _, m := range models {
			if m.GPTImage == nil {
				return refusedBy(m, "複雑さ・GIF・SVG の指定")
			}
		}
	}
	if markup >= 0 {
		for _, m := range models {
			if m.GPTImage == nil || !m.GPTImage.Markup {
				return refusedBy(m, "マークアップ")
			}
		}
	}
	if err := r.GPTImage.Check(); err != nil {
		return gterr.Invalid("%s", err.Error())
	}
	markups := 0
	for _, ref := range r.References {
		markups += btoi(ref.Role == gentestrepo.RoleMarkup)
	}
	if markups > 1 {
		return gterr.Invalid("マークアップは 1 枚だけ付けられます")
	}
	if markups > 0 && r.Mask != nil {
		return gterr.Invalid("マークアップは一部だけ描き直す（マスク）と一緒に使えません")
	}
	edit := len(r.References) - markups // the images of the edit: the markup is laid over the first of them
	if err := r.GPTImage.CheckRequest(edit > 0, markups > 0); err != nil {
		return gterr.Invalid("%s", err.Error())
	}
	return nil
}

// checkMarkupFiles judges the markup image against the image it is laid over (the first reference that is not the markup); sources are the files of
// r.References, in order. A request without a markup passes.
func checkMarkupFiles(r Request, sources []Source) error {
	markup := markupIndex(r)
	if markup < 0 || len(sources) != len(r.References) {
		return nil
	}
	base := slices.IndexFunc(r.References, func(ref RefInput) bool { return ref.Role != gentestrepo.RoleMarkup })
	if base < 0 {
		return nil // checkGPTImage refused it
	}
	mdata, err := os.ReadFile(sources[markup].Path) //nolint:gosec // the path comes from ResolveInput, under the gentest or project directory
	if err != nil {
		return gterr.Invalid("マークアップ画像のファイルが見つかりません")
	}
	bdata, err := os.ReadFile(sources[base].Path) //nolint:gosec // the same
	if err != nil {
		return gterr.Invalid("マークアップを重ねる元の画像のファイルが見つかりません")
	}
	return markupcheck.Check(mdata, bdata)
}

// svgRequested says the request asks an SVG of a model that makes one: its cost cannot be estimated (OpenAI charges the image model's rate for the reference image
// and the authoring model's rate for the SVG, and the second has no published price), so neither the estimate nor the result records a part of it.
func svgRequested(r Request, info modelcatalog.Info) bool {
	return info.GPTImage != nil && r.GPTImage.Format == gptimage.FormatSVG
}

// noPriceMessage is the warning of a model whose price is unknown: GPT Image 2.6 has no price per image (tokens are charged), and says so.
func noPriceMessage(info modelcatalog.Info) string {
	if info.GPTImage != nil {
		return info.Label + " は画像 1 枚あたりの公式の価格表がなく（トークン課金）、単価が分からないため、見積りの合計に入れません"
	}
	return info.Label + " は単価が分からないため、見積りの合計に入れません"
}
