package web_test

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
)

func renderHTML(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, c.Render(context.Background(), &sb))
	return sb.String()
}

var takeStatusLabels = map[domain.TakeStatus]string{
	domain.TakeCandidate: "候補", domain.TakeAIPick: "AI Pick", domain.TakeSelected: "採用", domain.TakeRejected: "除外",
}

// The Shot Inspector says a Shot's state and its Takes' statuses as the Shot card does, never by the
// internal enum value (#632, after #602).
func TestShotInspector_SaysStatesByTheirLabels(t *testing.T) {
	stateLabels := map[domain.ShotState]string{
		domain.ShotPlanning: "計画中", domain.ShotGenerating: "生成中", domain.ShotJudging: "Judge 中",
		domain.ShotRepairing: "Repair 中", domain.ShotEditing: "編集待ち",
	}
	var takes []domain.Take
	for s := range takeStatusLabels {
		takes = append(takes, domain.Take{ID: "t-" + string(s), ShotID: "s1", Status: s})
	}
	for state, label := range stateLabels {
		html := renderHTML(t, organisms.ShotInspector(organisms.ShotInspectorData{
			Project: domain.Project{ID: "p"}, Shot: domain.Shot{ID: "s1", SceneID: "sc", State: state}, Takes: takes,
		}))
		assert.Contains(t, html, "· "+label, "state %s", state)
		assert.NotContains(t, html, "· "+string(state), "state %s", state)
		for s, l := range takeStatusLabels {
			assert.Contains(t, html, ">"+l+"<", "take status %s", s)
			assert.NotContains(t, html, ">"+string(s)+"<", "take status %s", s)
		}
	}
}

// The Storage panel says a Take's status by its label, not by the internal enum value (#632).
func TestStoragePanel_SaysTakeStatusByItsLabel(t *testing.T) {
	var takes []domain.StoredTake
	for s := range takeStatusLabels {
		takes = append(takes, domain.StoredTake{TakeID: "t-" + string(s), ModelName: "m", Status: s, SizeBytes: 1024})
	}
	html := renderHTML(t, organisms.StoragePanel(organisms.StoragePanelData{ProjectID: "p", Storage: domain.StorageSummary{Takes: takes}}))
	for s, l := range takeStatusLabels {
		assert.Contains(t, html, "· "+l+" ·", "take status %s", s)
		assert.NotContains(t, html, "· "+string(s)+" ·", "take status %s", s)
	}
}

// The Brand Template list says how strictly a constraint is enforced as the constraint list does, never by
// the internal enum value (#644).
func TestBrandTemplatePanel_SaysEnforcementLevelsAsTheConstraintListDoes(t *testing.T) {
	html := renderHTML(t, organisms.BrandTemplatePanel(organisms.BrandTemplatePanelData{
		ProjectID: "p",
		Templates: []domain.BrandTemplate{{ID: "t1", Name: "T", Constraints: domain.BrandConstraints{
			{Type: domain.BrandForbidden, Level: domain.EnforceLock, Text: "x"},
			{Type: domain.BrandDisclaimer, Level: domain.EnforceAdvisory, Text: "y"},
		}}},
	}))
	assert.Contains(t, html, ">LOCK<")
	assert.Contains(t, html, ">Advisory<")
	assert.NotContains(t, html, "（lock）")
	assert.NotContains(t, html, "（advisory）")
}

// The Judge verdict is said in capitals in the Inspector and the Trace, as the Editor Judge and Final QC do (#644).
func TestJudgeVerdicts_AreSaidByTheirLabels(t *testing.T) {
	inspector := renderHTML(t, organisms.ShotInspector(organisms.ShotInspectorData{
		Project: domain.Project{ID: "p"}, Shot: domain.Shot{ID: "s1", SceneID: "sc"},
		Takes:  []domain.Take{{ID: "t1", ShotID: "s1"}},
		Judged: map[string]domain.JudgeResult{"t1": {Result: domain.JudgeFail, OverallConfidence: 0.5}},
	}))
	assert.Contains(t, inspector, "Judge: FAIL")
	assert.NotContains(t, inspector, "Judge: fail")

	trace := renderHTML(t, organisms.TakeTracePanel(domain.TakeTrace{
		Judgements: []domain.JudgeResult{{Result: domain.JudgePass}},
	}))
	assert.Contains(t, trace, ">PASS<")
	assert.NotContains(t, trace, ">pass<")
}
