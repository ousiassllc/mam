package modelcatalog

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/agegate"
)

// A model the age gate refuses is never listed (the lists leave it out); were it listed, the reason must say the gate, not a
// missing API key or an unreachable endpoint, whatever the source of the model.
func TestUnavailableMessageNamesTheAgeGateWhateverTheSource(t *testing.T) {
	for _, source := range []string{SourceLocal, SourceWorkflow, SourceChatGPT, ""} {
		assert.Contains(t, unavailableMessage(agegate.Excluded, source), "年齢確認がオフ", source)
	}
}
