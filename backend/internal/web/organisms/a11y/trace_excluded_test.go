package a11y_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
)

// The Trace says in words why a routing candidate was left out, including that the selected API provider does not serve the model; an unknown reason is shown as it is.
func TestTakeTracePanel_SaysWhyACandidateWasLeftOut(t *testing.T) {
	reasons := map[string]string{
		domain.ExcludedCircuitOpen:          "サーキットブレーカー OPEN",
		domain.ExcludedNoBackend:            "API キー未設定",
		domain.ExcludedProvider:             "選んだ API プロバイダーでは使えない",
		domain.ExcludedInsufficientEvidence: "実績不足",
		domain.ExcludedBelowMinimumScore:    "最低合格点に届かない",
		domain.ExcludedMargin:               "差が小さい",
		domain.ExcludedOutscored:            "他の候補が上位",
		"something_new":                     "something_new",
	}
	var candidates []domain.RoutingCandidate
	for reason := range reasons {
		candidates = append(candidates, domain.RoutingCandidate{Model: domain.ModelKling3, Excluded: reason})
	}

	html := renderHTML(t, organisms.TakeTracePanel(domain.TakeTrace{Routing: &domain.RoutingDecision{Candidates: candidates}}))

	for reason, want := range reasons {
		assert.Contains(t, html, "<td>"+want+"</td>", "reason %q", reason)
	}
}
