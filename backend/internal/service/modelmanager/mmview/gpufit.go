package mmview

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/vramfit"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// GPUVRAM is the dedicated video memory in bytes of the largest GPU of this PC (0 when none is known: no GPU was found, or the
// detection failed). The screen and the MCP tools judge a model's VRAM guideline (vramfit.For) against it. The detection starts a child
// process, so it is remembered for a few minutes (vramcache); what the GPU is doing now is never read through here.
func GPUVRAM(ctx context.Context, s *modelmanager.Service) uint64 {
	return s.VRAM.Get(ctx, s.Now(), s.GPU)
}

// ModelFit is how this PC's GPU stands to the VRAM guideline of one model of the catalog.
type ModelFit struct {
	Fit  vramfit.Fit `json:"fit" jsonschema:"unknown（GPU のメモリが分からない）/ short（目安に足りない）/ limited（動くが制約あり）/ enough（推奨を満たす）"`
	Text string      `json:"text" jsonschema:"画面と同じ言葉の判定。limited は制約の中身（量子化・タイル VAE・オフロードなど）を含む"`
}

// FitOf judges the model against a GPU with vramBytes of memory; ok is false when there is nothing to say: the memory is not known, or the
// model has no recommendation above its minimum. Only a model that says what a smaller GPU gives up (VRAM.Limits) is judged: the minimum of
// the others is mam's own conservative figure, and "動かない可能性が高い" from it would be a claim nobody measured.
func FitOf(v catalog.VRAM, vramBytes uint64) (ModelFit, bool) {
	fit := vramfit.For(v, vramBytes)
	return ModelFit{Fit: fit, Text: vramfit.Text(v, vramBytes)}, fit != vramfit.Unknown && v.RecommendedGB > v.MinGB
}
