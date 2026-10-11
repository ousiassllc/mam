package selector_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/web/selector"
)

func TestID_EscapesWhatMeansSomethingInASelector(t *testing.T) {
	for _, tc := range []struct{ prefix, id, want string }{
		{"shot-", "s1", "#shot-s1"},
		{"shot-", "a.b", `#shot-a\.b`},
		{"shot-", "a b", `#shot-a\ b`},
		{"msg-", "a:b#c", `#msg-a\:b\#c`},
		{"take-trace-slot-", "t[1]>x", `#take-trace-slot-t\[1\]\>x`},
		{"shot-", `a"b'c`, `#shot-a\"b\'c`},
		{"shot-", "日本_語-1", "#shot-日本_語-1"},
		{"shot-", "a\nb", `#shot-a\a b`},
		{"", "1a", `#\31 a`},
		{"", "-1", `#-\31 `},
		{"", "-", `#\-`},
		{"", "a\x00b", "#a\uFFFDb"},
	} {
		assert.Equal(t, tc.want, selector.ID(tc.prefix, tc.id), "%q+%q", tc.prefix, tc.id)
	}
}
