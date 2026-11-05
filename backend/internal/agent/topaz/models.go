package topaz

import (
	"github.com/ousiassllc/mam/backend/internal/agent/cloudenhance"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
)

// The ids of the models (the value of filters[].model), as the pages of developer.topazlabs.com name them.
const (
	ModelProteus         = "prob-4"  // https://developer.topazlabs.com/video-models/upscaling-and-enhancement/precision/proteus
	ModelIrisLowQuality  = "iris-3"  // https://developer.topazlabs.com/video-models/upscaling-and-enhancement/precision/iris-low-quality ("iris-2" is the Medium Quality page)
	ModelStarlightPrec26 = "slp-2.6" // https://developer.topazlabs.com/video-models/upscaling-and-enhancement/generative/starlight-precise-2.6
	ModelStarlightFast3  = "slf-3"   // https://developer.topazlabs.com/video-models/upscaling-and-enhancement/generative/starlight-fast-3
	ModelApollo          = "apo-8"   // https://developer.topazlabs.com/video-models/frame-interpolation/apollo
	ModelChronos         = "chr-2"   // https://developer.topazlabs.com/video-models/frame-interpolation/chronos
)

// license is what is known of the terms of the output of the API: the documentation says how the models are to be credited and nothing of who
// owns or may sell the output, so the answer is "unknown" (the user reads the terms), not a guess.
var license = modellicense.Info{
	Commercial:  modellicense.Unknown,
	LicenseName: "Topaz Labs API 利用規約（出力の権利は公式ドキュメントに記載なし）",
	SourceURL:   "https://developer.topazlabs.com/getting-started/faqs",
	CheckedAt:   "2026-11-04",
	Notice: "公式ドキュメント（FAQ）には、出力した動画の権利や商用利用の可否が書かれていません。商用で使う前に Topaz Labs の利用規約で確かめてください。" +
		"Topaz のモデルを自分のサービスの中で提供するときは、「Powered by Topaz Labs」のようにモデルの出典を表示することが求められ、" +
		"提携・推奨・共同所有を思わせる表現は使えません。処理は米国で行われ、元の動画と結果は Topaz に 7 日間保管されます。",
}

// models are the models of the Video API that mam offers: the ones that only retime or only enlarge a video, no denoising, SDR-to-HDR or
// creative models. The order is the order of the screens.
var models = []cloudenhance.CloudModel{
	{
		ID: ModelProteus, Provider: cloudenhance.ProviderTopaz, Op: cloudenhance.OpUpscale, Label: "Proteus（標準）",
		Description: "拡大・ノイズ除去・シャープ化を一度に行う標準のモデル。ふつうの映像から古い・粗い・圧縮された映像まで向く。迷ったらこれ。",
		License:     license,
	},
	{
		ID: ModelIrisLowQuality, Provider: cloudenhance.ProviderTopaz, Op: cloudenhance.OpUpscale, Label: "Iris（低画質の顔向け）",
		Description: "画質の低い映像（古いホームビデオ・強く圧縮された映像）の顔を取り戻しながら拡大するモデル。人物が主役のときに向く。",
		License:     license,
	},
	{
		ID: ModelStarlightPrec26, Provider: cloudenhance.ProviderTopaz, Op: cloudenhance.OpUpscale, Label: "Starlight Precise 2.6（高品質・生成型）",
		Description: "拡散モデルで 4K まで拡大する、画質重視のモデル。AI 生成動画や古い映像の顔・質感・文字をはっきりさせる。処理は遅く、クレジットも多め。",
		License:     license,
	},
	{
		ID: ModelStarlightFast3, Provider: cloudenhance.ProviderTopaz, Op: cloudenhance.OpUpscale, Label: "Starlight Fast 3（高速・生成型）",
		Description: "Starlight Precise 2.6 に近い品質で、最大 4 倍速く処理できる生成型のモデル。たくさん試すときや、先に仕上がりを確かめたいときに向く。",
		License:     license,
	},
	{
		ID: ModelApollo, Provider: cloudenhance.ProviderTopaz, Op: cloudenhance.OpInterpolate, Label: "Apollo（なめらか・精密）",
		Description: "フレームを補間して動きをなめらかにする。動きが加速・カーブするシーンや、少しぼけた動きにも強い。",
		License:     license,
	},
	{
		ID: ModelChronos, Provider: cloudenhance.ProviderTopaz, Op: cloudenhance.OpInterpolate, Label: "Chronos（自然な動き）",
		Description: "フレームを補間して動きをなめらかにする。実写の直線的な動きや重なった動きを自然に補間する。",
		License:     license,
	},
}
