package irodori_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/irodori"
)

func TestEmojify(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"a laugh in full-width parentheses": {"そうなんだ（笑）", "そうなんだ🤭"},
		"a sigh before the line":            {"（ため息）もういいよ", "😮‍💨もういいよ"},
		"half-width and square brackets":    {"ええ(咳払い)そうです[間]はい", "ええ🤧そうです⏸️はい"},
		"a big laugh is an emoji twice":     {"あはは（大笑い）", "あはは😆😆"},
		"a reading stays as it is":          {"東京（とうきょう）へ行く", "東京（とうきょう）へ行く"},
		"a remark of the script stays":      {"それは（もちろん）本当", "それは（もちろん）本当"},
		"a line without directions":         {"今日はいい天気ですね", "今日はいい天気ですね"},
		"two directions in one line":        {"（ささやく）だめ（泣く）", "👂だめ😭"},
		"spaces inside the direction":       {"はい（ 笑 ）", "はい🤭"},
		"an unclosed bracket is left":       {"はい（笑", "はい（笑"},
		"a long bracket is not a direction": {"はい（これは十二文字をこえる長い説明です）", "はい（これは十二文字をこえる長い説明です）"},
		"an empty line":                     {"", ""},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, irodori.Emojify(tc.in))
		})
	}
}
