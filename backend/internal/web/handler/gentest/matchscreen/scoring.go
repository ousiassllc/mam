package matchscreen

import (
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/match"
)

// skipText says why the face was left out of a score that was to have it.
var skipText = map[string]string{
	match.SkipUnavailable:     "顔のモデルが使えません",
	match.SkipReferenceNoFace: "参考の画像に顔が見つかりません",
	match.SkipGeneratedNoFace: "生成した画像に顔が見つかりません",
	match.SkipError:           "顔を測れませんでした",
}

// scoringText is how a score came about, in words: the points of the LLM and of the face with the weight of the face, or why the score
// is the LLM's alone. "" when the face is not mixed in.
func scoringText(s *match.Scoring) string {
	switch {
	case s == nil:
		return ""
	case s.Face != nil:
		f := s.Face
		llm := percent(1 - f.Weight)
		// the face share is what the LLM's leaves, so that the two shares add up to 100 % (rounded apart they can make 101 %)
		return fmt.Sprintf("LLM %d点 × %d%% ＋ 顔 %d点（一致度 %.2f）× %d%%", s.LLM, llm, f.Points, f.Cosine, 100-llm)
	}
	if why := skipText[s.Skipped]; why != "" {
		return fmt.Sprintf("LLM の点だけ（%s）", why)
	}
	return "LLM の点だけ"
}

func percent(v float64) int { return int(v*100 + 0.5) }
