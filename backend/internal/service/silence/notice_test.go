package silence_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/silence"
)

func TestNoticeNamesWhyNothingStarted(t *testing.T) {
	for name, tc := range map[string]struct {
		project       domain.Project
		pipelineOn    bool
		briefComplete bool
		want          string
		link          bool
	}{
		"AUTO without a pipeline":             {domain.Project{Mode: domain.ModeAuto, State: domain.StatePlanning}, false, true, "準備ができていない", true},
		"a complete Brief without a pipeline": {domain.Project{Mode: domain.ModeAuto, State: domain.StateDraft}, false, true, "準備ができていない", true},
		"waiting for an approval":             {domain.Project{Mode: domain.ModeAuto, State: domain.StateAwaitingApproval}, true, true, "承認待ち", false},
		"an incomplete Brief":                 {domain.Project{Mode: domain.ModeAuto, State: domain.StateDraft}, true, false, "Brief", false},
		"AUTO while generating":               {domain.Project{Mode: domain.ModeAuto, State: domain.StateGenerating}, true, true, "DIRECTOR モードに切り替えて", false},
		"DIRECTOR before the Brief is done":   {domain.Project{Mode: domain.ModeDirector, State: domain.StateDraft}, true, true, "Brief が完成して", false},
	} {
		t.Run(name, func(t *testing.T) {
			m := silence.Notice(tc.project, tc.pipelineOn, tc.briefComplete)
			assert.Equal(t, domain.RoleAssistant, m.Role)
			assert.Contains(t, m.Content, "保存しました")
			assert.Contains(t, m.Content, tc.want)
			assert.Equal(t, tc.link, m.RefType == domain.RefGlobalSettings)
		})
	}
}
