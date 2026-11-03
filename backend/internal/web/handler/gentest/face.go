package gentest

import (
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/facefusion"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
)

// skippedText says why the face similarity of a result has no number.
var skippedText = map[string]string{
	record.SkipReferenceNoFace: "参照の画像に顔が見つかりませんでした",
	record.SkipResultNoFace:    "出力の画像に顔が見つかりませんでした（小さい・横向き・隠れている）",
	record.SkipError:           "測れませんでした（画像の読み取りか顔のモデルの失敗）",
}

// similarityText is the face similarity of a result in words: the SFace number with the line it is read by.
func similarityText(s record.FaceSimilarity) string {
	if s.Skipped != "" {
		return skippedText[s.Skipped]
	}
	verdict := "同じ人と見なす目安 " + fmt.Sprintf("%.3f", s.Threshold) + " 未満"
	if s.SamePerson {
		verdict = "同じ人と見なす目安 " + fmt.Sprintf("%.3f", s.Threshold) + " 以上"
	}
	return fmt.Sprintf("%.3f（SFace のコサイン類似度。%s。目安であって、同じ人である保証ではありません）", s.Cosine, verdict)
}

// faceKVs are the rows of the detail that the face adds: the models and the cautions of a swap, and the similarity of the face of the result to
// the face reference it was made with.
func faceKVs(d record.ResultDetail) []view.KV {
	var out []view.KV
	if s := d.FaceSwap; s != nil {
		enhancer := s.Enhancer
		if enhancer == "" {
			enhancer = "復元しない"
		}
		out = append(out,
			view.KV{Key: "顔の入れ替え", Value: fmt.Sprintf("FaceFusion（入れ替え %s・顔検出 %s・復元 %s）", s.Swapper, s.Detector, enhancer)},
		)
		if len(s.Warnings) > 0 {
			out = append(out, view.KV{Key: "ライセンスの注意", Value: strings.Join(s.Warnings, " ／ ")})
		}
		out = append(out, view.KV{Key: "成人向けの画像", Value: facefusion.AdultNote})
	}
	if s := d.FaceSimilarity; s != nil {
		out = append(out, view.KV{Key: "顔の類似度", Value: similarityText(*s)})
	}
	return out
}
