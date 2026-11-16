package llmlocal_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/llmlocal"
)

// A server that cannot be used is told by the quiet chip (a light ground with the muted ink), not by the yellow badge.
func TestCard_AProblemIsAQuietChip(t *testing.T) {
	var sb strings.Builder
	require.NoError(t, llmlocal.Card(llmlocal.Data{State: "llama.cpp", Problem: "未導入です"}).Render(context.Background(), &sb))
	html := sb.String()

	assert.Contains(t, html, "llama.cpp（未導入です）")
	for _, yellow := range []string{"text-warning", "border-warning", "bg-warning"} {
		assert.NotContains(t, html, yellow)
	}
}
