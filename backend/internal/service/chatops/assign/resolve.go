package assign

import (
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/nlu"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

// purposeWords are the words that mean an image purpose, beside its ID.
var purposeWords = []struct {
	purpose modelsettings.ImagePurpose
	words   []string
}{
	{modelsettings.PurposeNewReference, []string{"新規"}},
	{modelsettings.PurposeEditReference, []string{"編集", "edit", "keyframe", "キーフレーム"}},
	{modelsettings.PurposePreview, []string{"preview", "プレビュー"}},
	{modelsettings.PurposeUnifiedOpenAI, []string{"unified", "統一"}},
}

// policyWords are the words that mean a Quality Policy, beside its ID.
var policyWords = []struct {
	policy domain.QualityPolicy
	words  []string
}{
	{domain.QualityMaxQuality, []string{"max"}},
	{domain.QualityBalanced, []string{"balanced", "バランス"}},
	{domain.QualityFast, []string{"fast"}},
}

// targetOf is the ID of the image purpose or the Quality Policy the text names ("新規参照", "Quality MAX", "max_quality"); "" for none.
func targetOf(text string) string {
	n := nlu.Norm(text)
	if n == "" {
		return ""
	}
	for _, p := range modelsettings.ImagePurposes() {
		if n == nlu.Norm(string(p)) {
			return string(p)
		}
	}
	for _, q := range domain.QualityPolicies() {
		if n == nlu.Norm(string(q)) {
			return string(q)
		}
	}
	for _, p := range purposeWords {
		if containsAny(n, p.words) {
			return string(p.purpose)
		}
	}
	for _, q := range policyWords {
		if containsAny(n, q.words) {
			return string(q.policy)
		}
	}
	return ""
}

func containsAny(n string, words []string) bool {
	for _, w := range words {
		if strings.Contains(n, nlu.Norm(w)) {
			return true
		}
	}
	return false
}

// pickModel finds the model the text names among names: the one that is called so, else every one whose name holds the text ("flux" is flux-2; "kling" is both Klings).
func pickModel(text string, names []string) []string {
	q := nlu.Norm(text)
	if q == "" {
		return nil
	}
	var holding []string
	for _, n := range names {
		switch nn := nlu.Norm(n); {
		case nn == q:
			return []string{n}
		case strings.Contains(nn, q):
			holding = append(holding, n)
		}
	}
	return holding
}

// pickResolution finds the resolution the text names among those the model has ("1080" and "1080P" are "1080p").
func pickResolution(text string, options []string) (string, bool) {
	q := strings.TrimSuffix(nlu.Norm(text), "p")
	for _, o := range options {
		if strings.TrimSuffix(nlu.Norm(o), "p") == q {
			return o, true
		}
	}
	return "", false
}
