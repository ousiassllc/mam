// Package charref is the chat operation that puts an image the director attached to a message on the face or the body slot of a Character (#1140;
// spec/components/chat-settings-operations.md §6): "この画像を主人公の顔にして" with an image attached gives a confirmation card, and accepting it registers the image in the Reference
// Library of the Project as a Character reference and assigns it to the slot, as the MCP tool assign_character_reference (WorldService.AssignReference) and the World State
// screen do. It changes this Project's Character only, never a setting of the whole app.
package charref

import (
	"regexp"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
)

// OpAssign is the operation of this package.
const OpAssign commandinterpreter.OpType = "assign_character_reference"

// The two slots of a Character (domain.ReferenceSlots).
const (
	SlotFace = "face"
	SlotBody = "body"
)

// Args of OpAssign. The model names the Character and the slot; the rest is filled in by the card (Handler.Question), from the images the message carried and the
// World State, so that the button's answer, which carries no attachment, knows what to use.
type Args struct {
	// Character is the name the director said ("" when none: the card asks, or takes the only Character).
	Character string `json:"character,omitempty"`
	// Slot is "face" or "body".
	Slot string `json:"slot,omitempty"`
	// CharacterID and AssetID are set by Question (never by the model: Question overwrites them) and carried by the yes button.
	CharacterID string `json:"character_id,omitempty"`
	AssetID     string `json:"asset_id,omitempty"`
}

func init() {
	commandinterpreter.Register(OpAssign, commandinterpreter.Spec{
		Example: `{"type": "assign_character_reference", "args": {"character": "ミオ", "slot": "face"}}`,
		Doc: `- "assign_character_reference": the director attached an image to the message and wants it to be the face or the body reference of a Character of the Project ` +
			`("この画像を主人公の顔にして", "添付の写真をミオの全身の参照にして"). "args.character" is the name of the Character as the director said it (leave it out when none is named), ` +
			`"args.slot" is "face" or "body". Never use it to change how a face looks in a Shot (that is a regeneration) and never invent a name.` + "\n",
		Clean: clean, Asked: Asks, Detect: detect,
		AnyMode: true, Confirm: true,
	})
}

// slotOf reads the slot the director named: 顔 / フェイス is the face, 体 / ボディ / 全身 the body; "" for none.
func slotOf(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "face", "顔", "フェイス", "顔写真":
		return SlotFace
	case "body", "体", "身体", "ボディ", "全身", "ボディー":
		return SlotBody
	}
	return ""
}

// clean keeps the name and the slot the model gave (a slot it invented is dropped: the card then asks) and what the card filled in.
func clean(op commandinterpreter.Operation) (commandinterpreter.Operation, bool) {
	var a Args
	if err := op.DecodeArgs(&a); err != nil {
		return commandinterpreter.Operation{}, false
	}
	a.Character, a.Slot = strings.TrimSpace(a.Character), slotOf(a.Slot)
	out, err := commandinterpreter.NewOperation(OpAssign, a)
	return out, err == nil
}

var (
	slotWord = regexp.MustCompile(`(?i)(顔|フェイス|face|全身|ボディ|身体|体|body)`)
	// imageWord is what is put there: the attachment ("この画像を", "添付の写真を") or a reference ("顔の参照に").
	imageWord = regexp.MustCompile(`(?i)(画像|写真|イメージ|絵|イラスト|参照|リファレンス|reference|image|photo|添付)`)
	// assignWord is what is done with it.
	assignWord = regexp.MustCompile(`(にして|にしたい|にする|として|に設定|を設定|設定して|に割り当て|割り当てて|に登録|にセット|に使って|を使って|に差し替|に変えて|にあてて|assign|set\b)`)
	// editsFace is a direction about how a face looks (笑顔にして, 顔を明るく), which is no reference.
	editsFace = regexp.MustCompile(`(笑顔|表情|明るく|暗く|きれい|綺麗|若く|老け|修正|補正|加工|ぼかし|ぼかして|美肌|メイク|髪|アップ|ズーム|向き|向かせ)`)
	// named finds the Character in "この画像を主人公の顔にして" / "主人公の顔の参照にこの画像を使って".
	named = regexp.MustCompile(`(?i)([^\s、。，,「」『』を]{1,30}?)\s*の\s*(顔写真|顔|フェイス|face|全身|ボディー|ボディ|身体|体|body)`)
)

// compoundBody is 体 inside a word (全体, 具体, 主体, 体験, 体力): not the body slot. 身体 is the slot and is kept (written as ボディ before this runs).
var compoundBody = regexp.MustCompile(`\p{Han}体|体\p{Han}`)

// slotText is the message as the slot words read it: 体 of a compound word is hidden, 身体 stays the body.
func slotText(message string) string {
	message = strings.ReplaceAll(message, "身体", "ボディ")
	return compoundBody.ReplaceAllStringFunc(message, func(m string) string { return strings.ReplaceAll(m, "体", "＊") })
}

// Asks reports whether the message asks to put an image on the face or the body of a Character: it names a slot and an image (or a reference), and what to do with them, and
// it is not a direction about how a face looks. A message that only mentions a face ("顔を笑顔にして") is not.
func Asks(message string) bool {
	message = slotText(message)
	return slotWord.MatchString(message) && imageWord.MatchString(message) && assignWord.MatchString(message) && !editsFace.MatchString(message)
}

// genericNames are the words that stand for "the Character" without naming one.
var genericNames = map[string]bool{
	"この": true, "その": true, "あの": true, "この画像": true, "その画像": true, "画像": true, "写真": true, "この写真": true, "添付": true, "添付の": true,
	"キャラ": true, "キャラクター": true, "登場人物": true, "この人": true, "この人物": true, "人物": true, "参照": true, "リファレンス": true,
}

func detect(message string) []commandinterpreter.Operation {
	if !Asks(message) {
		return nil
	}
	message = slotText(message)
	a := Args{Slot: slotOf(slotWord.FindString(message))}
	if m := named.FindStringSubmatch(message); m != nil {
		a.Character, a.Slot = nameOf(m[1]), slotOf(m[2])
	}
	op, err := commandinterpreter.NewOperation(OpAssign, a)
	if err != nil {
		return nil
	}
	return []commandinterpreter.Operation{op}
}

// nameOf strips what precedes the name in "この画像を主人公" and the words that name no one.
func nameOf(s string) string {
	if i := strings.LastIndex(s, "を"); i >= 0 {
		s = s[i+len("を"):]
	}
	for _, prefix := range []string{"この画像", "その画像", "この写真", "その写真", "添付の画像", "添付の写真", "添付画像", "添付写真", "画像", "写真"} {
		s = strings.TrimPrefix(s, prefix)
	}
	s = strings.Trim(strings.TrimSpace(s), "はをがにも ")
	if genericNames[s] {
		return ""
	}
	return s
}
