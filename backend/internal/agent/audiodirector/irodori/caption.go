package irodori

import (
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/textutil"
)

// maxCaptionRunes bounds the caption: the model reads a short description of the voice and the delivery (its examples are a sentence or
// two), and a long one only costs tokens.
const maxCaptionRunes = 200

// emotionPhrases turn the Scene Emotion (a word or a short phrase the Audio Director's planner wrote, such as 緊張 or 温かい安心感) into how
// the line is delivered. The first phrase whose key the emotion contains is used, so a longer key stands before the shorter one it contains.
var emotionPhrases = []struct{ key, phrase string }{
	{"安心", "温かく穏やかで、安心させるような話し方。"},
	{"緊張", "緊張して、声が少し張り詰めた話し方。"},
	{"不安", "不安げで、声が少し揺れる話し方。"},
	{"悲", "悲しげで、沈んだ声の話し方。"},
	{"切な", "切なげで、静かな話し方。"},
	{"怒", "怒りを抑えながら、強い調子で話す。"},
	{"高揚", "気持ちが高ぶって、弾んだ話し方。"},
	{"興奮", "興奮して、勢いのある話し方。"},
	{"喜", "嬉しそうで、明るく弾んだ話し方。"},
	{"楽し", "楽しそうで、明るい話し方。"},
	{"温かい", "温かく優しい話し方。"},
	{"穏や", "穏やかで落ち着いた話し方。"},
	{"静か", "静かで落ち着いた話し方。"},
	{"驚", "驚いた様子で、声が上ずる話し方。"},
	{"恐", "怯えて、声をひそめた話し方。"},
	{"希望", "前向きで、はっきりとした話し方。"},
}

// Caption builds the Voice Design caption of a line (the "irodori.caption" of the request): a short Japanese description of the voice and the
// delivery, from what the Character's settings say of the speaker and the Scene Emotion of the line. "" when there is nothing to say (the
// request then has no caption).
//
// cloned says the voice is copied from a reference recording. The model cards advise that a caption given with a reference only steers the
// emotion and the style and keeps the voice of the recording: a note on the voice (a young woman's voice) could contradict the recording and make
// the audio unstable. So with a reference the caption has the personality and the emotion; without one (a pure Voice Design) it also has the
// note on the voice, which then is what makes the voice.
func Caption(p localvoice.Profile, emotion string, cloned bool) string {
	var parts []string
	if v := sentence(p.Voice); v != "" && !cloned {
		parts = append(parts, v)
	}
	if per := strings.TrimSpace(p.Personality); per != "" {
		parts = append(parts, "性格は"+strings.TrimSuffix(per, "。")+"。")
	}
	if e := emotionPhrase(emotion); e != "" {
		parts = append(parts, e)
	}
	return textutil.TruncateRunes(strings.Join(parts, ""), maxCaptionRunes)
}

// sentence is s as one sentence with its full stop ("" for nothing).
func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !strings.HasSuffix(s, "。") {
		s += "。"
	}
	return s
}

// emotionPhrase is how a line of the Scene Emotion is delivered ("" for no emotion); an emotion the table does not know is said as it is.
func emotionPhrase(emotion string) string {
	emotion = strings.TrimSpace(emotion)
	if emotion == "" {
		return ""
	}
	for _, e := range emotionPhrases {
		if strings.Contains(emotion, e.key) {
			return e.phrase
		}
	}
	return "場面の感情は「" + strings.TrimSuffix(emotion, "。") + "」。それに合った話し方。"
}
