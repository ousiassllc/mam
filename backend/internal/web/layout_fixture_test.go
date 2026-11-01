package web_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/llm"
)

// updateFixturesEnv rewrites the layout fixtures instead of comparing with them.
const updateFixturesEnv = "MAM_UPDATE_FIXTURES"

// The wtest static/src/components/layout/layout.wtest.ts opens these files in a 375 px wide window with the real Tailwind
// CSS and checks that the page does not scroll sideways (#881, #882). The files are what the screens' components render, so
// the test measures the real markup, not a copy of it: when a component changes, this test fails until
// `MAM_UPDATE_FIXTURES=1 go test ./backend/internal/web -run TestLayoutFixtures` writes the new markup.
func TestLayoutFixtures_AreWhatTheComponentsRender(t *testing.T) {
	target := llm.Target{Provider: "anthropic", Model: "claude-sonnet-4-5-20250929"}
	fixtures := map[string]templ.Component{
		"layout-llm-section.html": llm.Section(llm.Data{
			Providers: []string{"anthropic", "openai"},
			Rows: []llm.Row{
				{Agent: "producer", Label: "Producer", Purpose: "企画の取りまとめ", Primary: target, Fallback: target, DefaultPrimary: target, DefaultFallback: target},
				{Agent: "director", Label: "Director", Purpose: "カットの演出", Primary: target, Fallback: llm.Target{Provider: "openai", Model: "gpt-5"}, DefaultPrimary: target, DefaultFallback: target, Restart: true},
			},
		}),
		"layout-hf-import.html": modelmgr.HFImportSection(modelmgr.HFImport{Kind: "text-to-image"}),
	}

	checkFixtures(t, fixtures)
}

// checkFixtures renders each component and compares it with its file in static/src/components/testdata (or writes it).
func checkFixtures(t *testing.T, fixtures map[string]templ.Component) {
	t.Helper()
	for name, c := range fixtures {
		t.Run(name, func(t *testing.T) {
			var sb strings.Builder
			require.NoError(t, c.Render(context.Background(), &sb))
			file := filepath.Join("..", "..", "..", "static", "src", "components", "testdata", name)
			if os.Getenv(updateFixturesEnv) != "" {
				require.NoError(t, os.WriteFile(file, []byte(sb.String()), 0o600)) // #nosec G306 -- a test fixture in the repository
				return
			}
			want, err := os.ReadFile(file) // #nosec G304 -- a fixed path inside the repository
			require.NoError(t, err, "write it with %s=1", updateFixturesEnv)
			assert.Equal(t, string(want), sb.String(), "the component changed: write the fixture again with %s=1", updateFixturesEnv)
		})
	}
}
