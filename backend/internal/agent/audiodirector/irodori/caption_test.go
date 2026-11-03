package irodori_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/irodori"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
)

func TestCaption_WithAReferenceTheVoiceNoteIsLeftOut(t *testing.T) {
	p := localvoice.Profile{Voice: "落ち着いた若い女性の声", Personality: "控えめで優しい"}

	cloned := irodori.Caption(p, "緊張", true)
	designed := irodori.Caption(p, "緊張", false)

	assert.Equal(t, "性格は控えめで優しい。緊張して、声が少し張り詰めた話し方。", cloned)
	assert.Equal(t, "落ち着いた若い女性の声。"+cloned, designed, "without a reference the note on the voice is what makes the voice")
}

func TestCaption(t *testing.T) {
	for name, tc := range map[string]struct {
		p       localvoice.Profile
		emotion string
		want    string
	}{
		"nothing to say":                    {localvoice.Profile{}, "", ""},
		"only an emotion":                   {localvoice.Profile{}, "温かい安心感", "温かく穏やかで、安心させるような話し方。"},
		"the first matching phrase is used": {localvoice.Profile{}, "悲しい怒り", "悲しげで、沈んだ声の話し方。"},
		"an emotion that is not in the table is said as it is": {localvoice.Profile{}, "ほろ苦い。", "場面の感情は「ほろ苦い」。それに合った話し方。"},
		"a personality that has a full stop":                   {localvoice.Profile{Personality: "明るい。"}, "", "性格は明るい。"},
		"blank settings are nothing":                           {localvoice.Profile{Voice: "  ", Personality: " "}, " ", ""},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, irodori.Caption(tc.p, tc.emotion, true))
		})
	}
}

func TestCaption_IsCutAtTwoHundredRunes(t *testing.T) {
	got := irodori.Caption(localvoice.Profile{Personality: strings.Repeat("あ", 500)}, "", true)

	assert.LessOrEqual(t, utf8.RuneCountInString(got), 200)
	assert.NotEmpty(t, got)
}
