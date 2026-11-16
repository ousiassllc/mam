package modelcards_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelcards"
)

// A model that cannot run without audio says so on its card, so the user attaches the file before choosing it; the others say nothing. The name of the role it
// reads in RequiresInputs is the service's.
func TestTheCardOfASpeechToVideoModelSaysItNeedsAudio(t *testing.T) {
	s2v := modelcatalog.Info{ID: "s2v", Media: "video", RequiresInputs: []string{gentest.RoleReference, gentest.RoleAudio}}
	card := modelcards.FromInfo(s2v)
	var labels []string
	for _, b := range card.Badges {
		labels = append(labels, b.Label)
	}
	assert.Contains(t, labels, "音声ファイルが必要（S2V）")

	for _, requires := range [][]string{nil, {gentest.RoleFirstFrame}, {gentest.RoleReference}} {
		m := modelcatalog.Info{ID: "other", Media: "video", RequiresInputs: requires}
		_, ok := modelcards.AudioBadge(m)
		assert.False(t, ok, "%v", requires)
	}
}
