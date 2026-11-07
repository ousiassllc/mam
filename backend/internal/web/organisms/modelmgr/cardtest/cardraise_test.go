// Package cardtest checks the templates of organisms/modelmgr as text (a package of its own because the directory of the templates is at its line budget).
package cardtest_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// controlTag is the opening tag of what a click lands on.
var controlTag = regexp.MustCompile(`(?s)<(?:a|button|input|select|textarea|summary)\b[^>]*>`)

// The name of a card is a button whose click area covers the whole card (modelcard.Item: after:absolute after:inset-0), so a link or a button in the
// card's body is covered by it and never pressed unless it is raised above it (relative z-raised; modelcard.Actions does it for the buttons). In every card of
// organisms/modelmgr, what is inside modelcard.Item and outside modelcard.Actions and is a control must carry both classes itself (#1110).
func TestTemplates_EveryControlInACardBodyIsRaisedAboveTheClickAreaOfTheCard(t *testing.T) {
	cards := 0
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".templ") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, body := range cardBodies(string(src)) {
			cards++
			for _, tag := range controlTag.FindAllString(body, -1) {
				assert.Contains(t, tag, "relative", "%s: a control in a card body is positioned to be raised: %s", path, tag)
				assert.Contains(t, tag, "z-raised", "%s: a control in a card body is covered by the card's click area: %s", path, tag)
			}
		}
		return nil
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, cards, 7, "the cards of the model manager were found (catalog, engine, extras, hf models, nodes, Ollama, runtimes)")
}

// cardBodies are the texts inside every `@modelcard.Item(...) { ... }` of a template without the `@modelcard.Actions() { ... }` blocks in them (found by the
// balance of the braces, line by line).
func cardBodies(src string) []string {
	var bodies []string
	lines := strings.Split(src, "\n")
	for i := 0; i < len(lines); i++ {
		if !strings.Contains(lines[i], "@modelcard.Item(") {
			continue
		}
		var body strings.Builder
		depth, skipFrom := 0, -1
		for ; i < len(lines); i++ {
			line := lines[i]
			if skipFrom < 0 && strings.Contains(line, "@modelcard.Actions()") {
				skipFrom = depth
			}
			if skipFrom < 0 && !strings.Contains(line, "@modelcard.Item(") {
				body.WriteString(line + "\n")
			}
			depth += strings.Count(line, "{") - strings.Count(line, "}")
			if skipFrom >= 0 && depth <= skipFrom {
				skipFrom = -1
			}
			if depth <= 0 {
				break
			}
		}
		bodies = append(bodies, body.String())
	}
	return bodies
}
