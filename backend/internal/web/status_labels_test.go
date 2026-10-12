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
