// Package assign is the chat operation of the image / video model assignment (#1140; spec/components/chat-settings-operations.md §3): the director asks for "生成の画像モデルの
// 新規参照 を flux にして", "Quality MAX の動画モデルを kling-4.0 に", "1080p で 3 Take" and gets a card that shows the route before and after; the answer saves it through the same
// modelsettings.Service.Save as the settings screen and the MCP tool set_model_assignment: the saved assignment is read, only the named purpose or Quality Policy changes, and the
// whole assignment is saved. The assignment is a setting of the whole app, which the card says.
package assign

import (
	"regexp"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
)

// OpSetModelAssignment changes one route of the image / video model assignment.
const OpSetModelAssignment commandinterpreter.OpType = "set_model_assignment"

// The roles of a route.
const (
	rolePrimary  = "primary"
	roleFallback = "fallback"
)

// Args of the operation. Target is an image purpose (modelsettings.ImagePurposes) or a Quality Policy (domain.QualityPolicies), "" when the message names none (the chat then says
// what to name). Model is the name the director said: Question resolves it against the models the assignment offers.
type Args struct {
	Target string `json:"target,omitempty"`
	Role   string `json:"role,omitempty"`
	Model  string `json:"model,omitempty"`
	// Fast chooses the fast variant of an image model; nil leaves it as it is (false when the model changes).
	Fast *bool `json:"fast,omitempty"`
	// Resolution and Takes are those of a Quality Policy's video route; "" and 0 leave them as they are.
	Resolution string `json:"resolution,omitempty"`
	Takes      int    `json:"takes,omitempty"`
}

func init() {
	commandinterpreter.Register(OpSetModelAssignment, commandinterpreter.Spec{
		Example: `{"type": "set_model_assignment", "args": {"target": "max_quality", "role": "primary", "model": "kling-4.0", "resolution": "1080p", "takes": 3}}`,
		Doc: `- "set_model_assignment": the director wants the image model of a purpose, or the video model, resolution or number of Takes of a Quality Policy, assigned differently ` +
			`("生成の画像モデルの 新規参照 を flux にして", "Quality MAX の動画モデルを kling-4.0 に", "1080p で 3 Take", "BALANCED の代替を seedance-2.0 に"). ` +
			`"args.target" is the image purpose "new_reference", "edit_reference", "preview" or "unified_openai", or the Quality Policy "fast", "balanced" or "max_quality" ` +
			`(leave it out when the message names none). "args.role" is "primary" (default) or "fallback" (代替). "args.model" is the model name as the director said it; do not complete it ` +
			`to a full identifier. For an image purpose "args.fast" true or false chooses the fast variant. For a Quality Policy "args.resolution" is the resolution of the video model ` +
			`("1080p") and "args.takes" the number of Takes. Name only what the director names. It is not the choice of the Project's own Quality Policy and not the LLM of an Agent.` + "\n",
		Clean:   clean,
		Asked:   Asked,
		AnyMode: true, Confirm: true, Global: true,
	})
}

// clean keeps the operation the model returned with its arguments in their normal form: the target as an ID, the role as primary or fallback, no negative Takes. What names nothing
// valid is left empty, not guessed: the chat then asks.
func clean(op commandinterpreter.Operation) (commandinterpreter.Operation, bool) {
	var a Args
	if err := op.DecodeArgs(&a); err != nil {
		return commandinterpreter.Operation{}, false
	}
	a.Target, a.Role = targetOf(a.Target), roleOf(a.Role)
	a.Model, a.Resolution = strings.TrimSpace(a.Model), strings.TrimSpace(a.Resolution)
	if a.Takes < 0 {
		a.Takes = 0
	}
	out, err := commandinterpreter.NewOperation(OpSetModelAssignment, a)
	return out, err == nil
}

// roleOf is primary unless the text says the alternative ("fallback", "代替", "フォールバック", "バックアップ").
func roleOf(text string) string {
	n := norm(text)
	if strings.Contains(n, "fallback") || strings.Contains(n, "代替") || strings.Contains(n, "フォールバック") || strings.Contains(n, "バックアップ") {
		return roleFallback
	}
	return rolePrimary
}

var (
	// mediaWord is what makes the message about the image / video models: the media, a purpose or Quality Policy, or the name of such a model. "モデル" alone is not enough: the LLM of an
	// Agent is a model too (the operation of package llmroute).
	mediaWord  = regexp.MustCompile(`(?i)(画像|動画|イメージ|image|video|参照|リファレンス|keyframe|キーフレーム|プレビュー|preview|quality|クオリティ|品質|nano.?banana|flux|gpt.?image|kling|seedance|runway|minimax|wan.?3|luma|gemini.?omni)`)
	changeWord = regexp.MustCompile(`(?i)(割り当て|割当|にして|にする|にしたい|に変|へ変|変えて|変更|切り替|切替|設定|使って|使いたい|使う|set|assign|switch|use|[にへ]\s*[。.!！]?\s*$)`)
	policyWord = regexp.MustCompile(`(?i)(quality|クオリティ|品質|fast|balanced|max)`)
	takeWord   = regexp.MustCompile(`(?i)(\d+\s*(take|テイク)|\d{3,4}\s*p\b|解像度)`)
	// otherOp is what another operation owns: the API key of a model's service and the API provider the cloud video models are called through (package apikey).
	otherOp = regexp.MustCompile(`(?i)(api\s*キー|apiキー|api\s*key|経由|openrouter|fal\.ai|replicate|プロバイダー|provider)`)
	inquiry = regexp.MustCompile(`(?i)(教えて|見せて|一覧|確認して|とは|何です|なんです|[かが][？?]|\?|？)\s*$`)
)

// Asked reports whether the message asks to change the assignment: it names a model (or a resolution, or a number of Takes) and does something to it. A question about the
// assignment ("今の画像モデルは何？") is not.
func Asked(message string) bool {
	if inquiry.MatchString(strings.TrimSpace(message)) || otherOp.MatchString(message) {
		return false
	}
	if mediaWord.MatchString(message) && changeWord.MatchString(message) {
		return true
	}
	// "1080p で 3 Take": a Quality Policy's resolution and Takes, said on their own
	return takeWord.MatchString(message) && (policyWord.MatchString(message) || changeWord.MatchString(message) || strings.Contains(message, "で"))
}
