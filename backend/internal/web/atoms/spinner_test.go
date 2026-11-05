package atoms_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
)

// The spinner turns for the whole generation, also when the OS asks for reduced motion: it says that something is in progress, which is not
// a decoration (the UI guide, M3).
func TestSpinner_KeepsSpinningUnderReducedMotion(t *testing.T) {
	var sb strings.Builder
	require.NoError(t, atoms.Spinner("生成中").Render(context.Background(), &sb))

	assert.Contains(t, sb.String(), "animate-spin")
	assert.NotContains(t, sb.String(), "motion-reduce:animate-none")
	assert.Contains(t, sb.String(), `aria-label="生成中"`)
}
