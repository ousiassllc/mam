// Package intentinfo lists the "やりたいこと" of the generation test (a request's intent) with the model each picks now, for list_generation_models (Issue #977).
package intentinfo

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/intent"
)

// IntentInfo is one "やりたいこと" (a request's intent) with the model mam picks for it now (Issue #977).
type IntentInfo struct {
	ID    string `json:"id" jsonschema:"リクエストの intent に使う値"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
	// NeedsReferences: the intent is made from input images (the same face): references が 1 枚以上要る。
	NeedsReferences bool `json:"needs_references,omitempty" jsonschema:"true のとき references（顔の写真）が 1 枚以上要る。役割 reference は face として使う"`
	// Model is the model picked now for a request without input images ("" when none suits: Reason says why).
	Model      string `json:"model,omitempty" jsonschema:"いま選ばれるモデルの id（入力画像なしのとき）。導入済み・選べる・商用の絞り込みと OS・GPU の条件に合うものの先頭。入力画像があると画像を使えるモデルから選ぶ"`
	ModelLabel string `json:"model_label,omitempty"`
	Note       string `json:"note,omitempty" jsonschema:"そのモデルに添える注意（専用のモデルが無く汎用で代える、など）"`
	Reason     string `json:"reason,omitempty" jsonschema:"選べるモデルが無い理由と、どこで導入するか"`
}

// List lists the intents with the model each picks now.
func List(ctx context.Context, g *gentest.Service) []IntentInfo {
	var out []IntentInfo
	for _, in := range intent.All() {
		info := IntentInfo{ID: in.ID, Label: in.Label, Hint: in.Hint, NeedsReferences: in.NeedsReferences}
		if rec, err := g.Recommend(ctx, in.ID, in.NeedsReferences); err == nil {
			info.Model, info.ModelLabel, info.Note, info.Reason = rec.Model.ID, rec.Model.Label, rec.Note, rec.Reason
		}
		out = append(out, info)
	}
	return out
}
