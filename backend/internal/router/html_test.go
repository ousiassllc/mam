package router_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

// inputChecked reports whether body has an <input> with value=value that is checked. Attribute
// order is not part of the markup contract, so this looks at the whole tag.
func inputChecked(body, value string) bool {
	for _, tag := range regexp.MustCompile(`<input[^>]*>`).FindAllString(body, -1) {
		if regexp.MustCompile(`\svalue="`+regexp.QuoteMeta(value)+`"`).MatchString(tag) &&
			regexp.MustCompile(`\schecked[\s>=]`).MatchString(tag) {
			return true
		}
	}
	return false
}

func assertChecked(t *testing.T, body, value string, msgAndArgs ...any) {
	t.Helper()
	assert.True(t, inputChecked(body, value), append([]any{"expected a checked input with value " + value}, msgAndArgs...)...)
}

func assertNotChecked(t *testing.T, body, value string) {
	t.Helper()
	assert.False(t, inputChecked(body, value), "unexpected checked input with value "+value)
}
