package pages_test

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/editorpanel"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/sceneshot"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/overview"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/shotinspector"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
)

// Every auxiliary view has exactly one h1 that names the page; the sections of its organisms start at h2 (#645).
func TestAuxiliaryPages_HaveOneH1(t *testing.T) {
	project := domain.Project{ID: "p", Name: "P"}
	tests := []struct {
		name string
		page templ.Component
		h1   string
	}{
		{"global settings", pages.GlobalSettingsPage("tok", pages.GlobalSettingsPageData{Title: "設定", Body: pages.SettingsOverviewBody(overview.Data{})}), "設定"},
		{"editor", pages.EditorPage("tok", editorpanel.Data{Project: project}), "編集"},
		{"take comparison", pages.TakeComparisonPage("tok", organisms.TakeComparisonData{Project: project, Shot: domain.Shot{ID: "s"}}), "Take 比較"},
		{"branch", pages.BranchPage("tok", organisms.BranchPageData{Project: project}), "Branch"},
		{"scene shot", pages.SceneShotPage("tok", sceneshot.Data{Project: project}), "Scene / Shot"},
		{"shot inspector", pages.ShotInspectorPage("tok", shotinspector.Data{Project: project}), "Shot Inspector"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var sb strings.Builder

			require.NoError(t, tc.page.Render(context.Background(), &sb))

			assert.Equal(t, 1, strings.Count(sb.String(), "<h1 "), "number of h1")
			assert.Contains(t, sb.String(), ">"+tc.h1+"</h1>")
		})
	}
}
