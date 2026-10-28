package direct_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/direct"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestAspectProblemNamesTheRatiosAModelCannotMake(t *testing.T) {
	for _, tt := range []struct {
		name       string
		model      domain.ModelName
		aspect     string
		firstFrame bool
		refused    bool
	}{
		{"Runway makes 16:9", domain.ModelRunwayGen45, domain.AspectRatio16x9, false, false},
		{"Runway makes 1:1 from a first frame", domain.ModelRunwayGen45, domain.AspectRatio1x1, true, false},
		{"Runway refuses 1:1 without a first frame", domain.ModelRunwayGen45, domain.AspectRatio1x1, false, true},
		{"Omni makes 16:9", domain.ModelGeminiOmniFlash, domain.AspectRatio16x9, false, false},
		{"Omni makes 9:16", domain.ModelGeminiOmniFlash, domain.AspectRatio9x16, true, false},
		{"Omni refuses 1:1 even from a first frame", domain.ModelGeminiOmniFlash, domain.AspectRatio1x1, true, true},
		{"Kling takes any ratio", domain.ModelKling3, domain.AspectRatio1x1, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			problem := direct.AspectProblem(tt.model, tt.aspect, tt.firstFrame)
			assert.Equal(t, tt.refused, problem != "", problem)
		})
	}
}
