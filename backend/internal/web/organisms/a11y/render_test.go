package a11y_test

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/require"
)

func renderHTML(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, c.Render(context.Background(), &sb))
	return sb.String()
}
