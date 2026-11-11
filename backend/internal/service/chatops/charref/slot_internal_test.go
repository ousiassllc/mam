package charref

import (
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

// detect reads the name from the message by the offsets of the masked text, so a masked word must keep its byte length.
func TestNotBody_EveryWordKeepsTheByteLength(t *testing.T) {
	assert.Equal(t, 2, utf8.RuneCountInString(mask))
	for _, w := range notBodyWords {
		assert.Equal(t, 2, utf8.RuneCountInString(w), w)
		assert.Len(t, slotText(w), len(w), w)
		assert.Equal(t, mask, slotText(w), w)
	}
}
