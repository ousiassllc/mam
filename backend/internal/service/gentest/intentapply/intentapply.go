// Package intentapply puts the model an intent picks into a request of the generation test. It is a package of its own so that package gentest stays under
// the line limit; it uses the service only through its exported methods.
package intentapply

import (
	"context"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/intent"
)

// Apply fills the models of r from the intent id: the model gentest.Service.Recommend picks for r's input images. A request that names models as well
// is refused (the intent is the way to leave the choice to mam), and so is an intent that suits no installed model, or one made from an input
// image (the same face) when there is none. The role "reference" of an input image becomes "face" for the same-face intent, whose
// models read it as the face to keep. r is returned changed, not modified.
func Apply(ctx context.Context, g *gentest.Service, r gentest.Request, id string) (gentest.Request, intent.Recommendation, error) {
	if len(r.Models) > 0 {
		return r, intent.Recommendation{}, gterr.Invalid("models と intent は同時に指定できません（intent はモデルを mam が選ぶ指定です）")
	}
	if r.Media != "" && r.Media != gentest.MediaImage {
		return r, intent.Recommendation{}, gterr.Invalid("intent は画像だけで指定できます")
	}
	rec, err := g.Recommend(ctx, id, len(r.References) > 0)
	if err != nil {
		return r, rec, err
	}
	if rec.Intent.NeedsReferences && len(r.References) == 0 {
		return r, rec, gterr.Invalid("「%s」は入力画像（顔の写真）から作ります。入力画像を 1 枚以上入れてください", rec.Intent.Label)
	}
	if !rec.Picked() {
		return r, rec, gterr.Invalid("%s", rec.Reason)
	}
	r.Models = []string{rec.Model.ID}
	if rec.Intent.NeedsRole == localcfg.RoleFace {
		refs := slices.Clone(r.References)
		for i, ref := range refs {
			if ref.Role == "" || ref.Role == localcfg.RoleWhole {
				refs[i].Role = localcfg.RoleFace
			}
		}
		r.References = refs
	}
	return r, rec, nil
}
