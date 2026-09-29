package pages_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/pages"
)

func TestHomePage_PublishesEscapedCSRFTokenInMetaTag(t *testing.T) {
	var sb strings.Builder
	require.NoError(t, pages.HomePage(`t"><script>alert(1)</script>`).Render(context.Background(), &sb))
	html := sb.String()

	assert.Contains(t, html, `<meta name="csrf-token" content="t&#34;&gt;&lt;script&gt;alert(1)&lt;/script&gt;">`)
	assert.NotContains(t, html, "<script>alert(1)")
	assert.Contains(t, html, `<script type="module" src="/static/app.js">`)
}
