package atoms_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
)

// The spinner turns for the whole generation; it stops when the OS asks for reduced motion (#797).
func TestSpinner_StopsSpinningUnderReducedMotion(t *testing.T) {
	var sb strings.Builder
	require.NoError(t, atoms.Spinner("生成中").Render(context.Background(), &sb))

	assert.Contains(t, sb.String(), "animate-spin")
	assert.Contains(t, sb.String(), "motion-reduce:animate-none")
	assert.Contains(t, sb.String(), `aria-label="生成中"`)
}
