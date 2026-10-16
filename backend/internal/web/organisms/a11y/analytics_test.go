package a11y_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
)

// The page's h1 is followed by h2, not h3: a skipped level breaks heading navigation (#781).
func TestAnalyticsPanel_HeadingsFollowTheH1WithH2(t *testing.T) {
	html := renderHTML(t, organisms.AnalyticsPanel(domain.ProductionMetrics{}))

	assert.NotContains(t, html, "<h3")
	assert.GreaterOrEqual(t, strings.Count(html, "<h2"), 2, "the North Star and each metric group have an h2")
}
