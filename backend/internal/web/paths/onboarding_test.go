package paths_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func TestOnboardingPaths(t *testing.T) {
	assert.Equal(t, "/onboarding", paths.Onboarding())
	assert.Equal(t, "/onboarding/flow", paths.OnboardingStep(paths.OnboardingFlow))
	assert.Equal(t, "/onboarding/a%2Fb", paths.OnboardingStep("a/b"))
	assert.Equal(t, "/onboarding/finish", paths.OnboardingFinish())
	assert.Equal(t, "/onboarding/credentials/status", paths.OnboardingCredentialsStatus())
	assert.Equal(t, "/onboarding/codex-path", paths.OnboardingCodexPath())
}
