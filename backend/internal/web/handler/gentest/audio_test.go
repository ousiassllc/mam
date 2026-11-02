package gentest_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// The help of the model cards says which video models make the sound with the picture (Seedance 2.0 does, and LTX-2.5 once it is installed: its workflows say native_audio), so that a
// result that plays with sound is not a surprise and one that is silent is not a fault; Runway makes no sound.
func TestModelHelpSaysWhichVideoModelsMakeTheSoundWithThePicture(t *testing.T) {
	s := newScreen(t)
	body := s.get(paths.GenerationTestPart("form") + "?media=video").Body.String()
	help := regexp.MustCompile(`(?s)data-testid="gt-models-help">.*?</ul>`).FindString(body)
	require.NotEmpty(t, help)
	item := func(id string) string {
		m := regexp.MustCompile(`(?s)<li[^>]*>(?:(?:[^<]|<[^/]|</[^l])*?)` + regexp.QuoteMeta(id) + `.*?</li>`).FindString(help)
		require.NotEmpty(t, m, id)
		return m
	}
	assert.Contains(t, item("seedance-2.0"), "音声つき")
	assert.NotContains(t, item("runway-gen-4.5"), "音声つき")
}
