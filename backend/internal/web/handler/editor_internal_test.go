package handler

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
)

func renderActions(t *testing.T, v service.EditorView) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, organisms.EditorActions(editorPanelData(v)).Render(context.Background(), &sb))
	return sb.String()
}

// The additional Shots still being made reach the panel (#338): it tells how many are being made
// and hides the request button, which the server refuses (409) until they are done.
func TestEditorPanelData_CarriesThePendingAdditionalShots(t *testing.T) {
	judge := &domain.EditorJudgeResult{Verdict: domain.EditorNeedShot}
	v := service.EditorView{
		Project:  domain.Project{ID: "p", State: domain.StateEditJudging},
		Timeline: &service.TimelineView{EditTimelineVersion: domain.EditTimelineVersion{ID: "tl1"}, Judge: judge},
	}
	assert.Contains(t, renderActions(t, v), "request-additional-shot", "nothing pending: the request is offered")
	assert.NotContains(t, renderActions(t, v), "data-pending-shots")

	v.PendingShots = 2
	body := renderActions(t, v)
	assert.Contains(t, body, "data-pending-shots")
	assert.Contains(t, body, "追加 Shot を 2 件生成中です")
	assert.NotContains(t, body, "request-additional-shot", "a request is refused while the last round is not done")
}

// A stage may move the Project: the answer carries the state badge of the page header (#348).
func TestEditorActionsFragment_RefreshesTheStateBadge(t *testing.T) {
	var sb strings.Builder
	d := editorPanelData(service.EditorView{Project: domain.Project{ID: "p", State: domain.StateEditJudging}})
	require.NoError(t, organisms.EditorActionsFragment(d).Render(context.Background(), &sb))
	body := sb.String()
	assert.Contains(t, body, `id="editor-actions"`)
	assert.Regexp(t, `<span[^>]*id="project-state"[^>]*hx-swap-oob="true"[^>]*data-state="EDIT_JUDGING"|<span[^>]*id="project-state"[^>]*data-state="EDIT_JUDGING"[^>]*hx-swap-oob="true"`, body)
}
