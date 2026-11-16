// Package shotaudio is the chat operation that ties an audio file to a Shot or takes it off (Issue #1153; spec/components/chat-settings-operations.md §0.6): "この音声を Shot 2 に
// 使って" with an audio file attached, and "Shot 2 の音声を外して". The Shot is made from the sound by a speech-to-video model afterwards (Wan 2.2 S2V). It changes the Shot only,
// at once and without a card (it is as easy to undo as a LOCK is, and it spends nothing: the generation is a separate direction), through shotaudiosvc.Service, the function the
// Shot screen and the MCP tool set_shot_audio call.
package shotaudio

import (
	"regexp"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
)

// OpSetShotAudio is the operation of this package.
const OpSetShotAudio commandinterpreter.OpType = "set_shot_audio"

// The actions of Args.
const (
	// ActionAttach uses the audio file the message carries for the Shot.
	ActionAttach = "attach"
	// ActionClear takes the audio off the Shot.
	ActionClear = "clear"
)

// Args of OpSetShotAudio.
type Args struct {
	Action string `json:"action"`
}

func init() {
	commandinterpreter.Register(OpSetShotAudio, commandinterpreter.Spec{
		Example: `{"type": "set_shot_audio", "args": {"action": "attach"}}`,
		Doc: `- "set_shot_audio": the director wants the sound of one shot to be an audio file (a voice or a song that the shot's clip is made from: "この音声を Shot 2 に使って", ` +
			`"このナレーションに合わせて口を動かして") or wants the audio taken off the shot ("Shot 2 の音声を外して"). "args.action" is "attach" (use the audio file attached to the message) ` +
			`or "clear" (take the shot's audio off). It never changes the soundtrack of the whole video (that is the audio director's) and never invents a file: without an attached audio file ` +
			`"attach" is refused.` + "\n",
		Clean: clean, Asked: Asks, Immediate: true, Shot: true,
	})
}

// clean keeps an action it knows; an unknown one is dropped (the model's word alone is never guessed into "clear").
func clean(op commandinterpreter.Operation) (commandinterpreter.Operation, bool) {
	var a Args
	if err := op.DecodeArgs(&a); err != nil {
		return commandinterpreter.Operation{}, false
	}
	switch a.Action = strings.ToLower(strings.TrimSpace(a.Action)); a.Action {
	case ActionAttach, ActionClear:
		out, err := commandinterpreter.NewOperation(OpSetShotAudio, a)
		return out, err == nil
	}
	return commandinterpreter.Operation{}, false
}

// audioWord is what the message must talk about: a model that returns "clear" for "カメラを外して" is not carried out.
var audioWord = regexp.MustCompile(`(?i)(音声|音源|オーディオ|\baudio\b|ナレーション|セリフ|台詞|ボイス|\bvoice\b|音楽|サウンド|\bsound\b|\bwav\b|\bmp3\b|\bflac\b|\bogg\b|\bm4a\b|口パク|口を動か|喋|しゃべ|歌)`)

// Asks reports whether the message talks about audio.
func Asks(message string) bool { return audioWord.MatchString(message) }
