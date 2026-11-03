package irodori

import (
	"regexp"
	"strings"
)

// cues are the stage directions of a script that Irodori-TTS can act, and the emoji it takes for them (the emoji list of the model cards:
// EMOJI_ANNOTATIONS.md of the model repository); a direction is matched as a whole, and one word is in one row only. The model cards say an
// emoji repeated is emphasised, which is how a big laugh differs from a small one. The table is in spec/architecture/model-adapters-m3.md §14.11.
var cues = []struct {
	words []string
	emoji string
}{
	{[]string{"大笑い", "爆笑", "高笑い"}, "😆😆"},
	{[]string{"苦笑い", "苦笑", "含み笑い", "くすくす", "笑い", "笑"}, "🤭"},
	{[]string{"ため息", "ため息をつく", "溜息", "溜め息", "吐息", "深呼吸"}, "😮‍💨"},
	{[]string{"咳払い", "咳き込む", "咳", "せき", "くしゃみ", "鼻をすする"}, "🤧"},
	{[]string{"ささやく", "囁く", "囁き", "ささやき", "小声"}, "👂"},
	{[]string{"沈黙", "無言", "間"}, "⏸️"},
	{[]string{"すすり泣き", "泣き声", "泣く", "泣き", "嗚咽"}, "😭"},
	{[]string{"悲鳴", "絶叫", "叫ぶ", "叫び"}, "😱"},
	{[]string{"息をのむ", "息を呑む"}, "😮"},
	{[]string{"驚く", "驚き", "感嘆"}, "😲"},
	{[]string{"あくび", "欠伸"}, "🥱"},
	{[]string{"舌打ち"}, "😒"},
	{[]string{"照れる", "照れ", "恥ずかしそうに"}, "🫣"},
	{[]string{"呆れ", "呆れる", "呆れて"}, "🙄"},
	{[]string{"怒って", "怒り", "不満げに"}, "😠"},
	{[]string{"心配そうに", "心配"}, "😟"},
	{[]string{"慌てて", "動揺", "どもる"}, "😰"},
	{[]string{"安堵", "ほっとして"}, "😌"},
	{[]string{"鼻歌", "ハミング"}, "🎵"},
	{[]string{"電話越し", "電話口"}, "📞"},
	{[]string{"ゆっくり", "ゆっくりと"}, "🐢"},
	{[]string{"早口", "急いで"}, "⏩"},
	{[]string{"懇願", "懇願して"}, "🙏"},
	{[]string{"優しく"}, "🫶"},
	{[]string{"疑問", "首をかしげて"}, "🤔"},
}

// directionPattern finds a stage direction: a word in full-width or half-width parentheses or square brackets (（笑） (ため息) 【咳払い】 [間]).
var directionPattern = regexp.MustCompile(`[（(［\[【]\s*([^（）()［］\[\]【】\n]{1,12}?)\s*[）)］\]】]`)

// cueEmoji maps the written direction to its emoji (false for a word that is not one of the table).
var cueEmoji = func() map[string]string {
	m := map[string]string{}
	for _, c := range cues {
		for _, w := range c.words {
			m[w] = c.emoji
		}
	}
	return m
}()

// Emojify puts the emoji for the stage directions of a script line in the text, in the place of the direction: 「そうなんだ（笑）」 becomes
// 「そうなんだ🤭」 and 「（ため息）もういいよ」 becomes 「😮‍💨もういいよ」. Only a direction that is in the table is replaced; any other bracketed
// text (a reading such as 「東京（とうきょう）」, a remark) stays as it is, because the voice reads it aloud as written. A line without
// directions is returned as it is.
func Emojify(text string) string {
	if !strings.ContainsAny(text, "（()［[【") {
		return text
	}
	return directionPattern.ReplaceAllStringFunc(text, func(m string) string {
		inner := directionPattern.FindStringSubmatch(m)[1]
		if e, ok := cueEmoji[strings.TrimSpace(inner)]; ok {
			return e
		}
		return m
	})
}
