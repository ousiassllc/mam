package pages_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
)

// The Inspector's way back leads to a page that exists: a Branch's Scene is not on the trunk's Scene pages (#399).
func TestShotInspectorPage_BackLinkFollowsTheBranchOfTheScene(t *testing.T) {
	tests := []struct {
		name, branchID, href, label string
	}{
		{"trunk", "", `href="/projects/p1/scenes/sc1"`, "← Scene / Shot に戻る"},
		{"branch", "b1", `href="/branches/b1"`, "← Branch に戻る"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var sb strings.Builder

			require.NoError(t, pages.ShotInspectorPage("tok", organisms.ShotInspectorData{
				Project: domain.Project{ID: "p1"},
				Scene:   domain.Scene{ID: "sc1", ProjectID: "p1", BranchID: tc.branchID},
				Shot:    domain.Shot{ID: "s1", SceneID: "sc1"},
			}).Render(context.Background(), &sb))

			assert.Contains(t, sb.String(), tc.href)
			assert.Contains(t, sb.String(), tc.label)
		})
	}
}
