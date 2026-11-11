// Package apikey is the chat operation of the API keys and of the API provider of the cloud video models (#1140; spec/components/chat-settings-operations.md): the director
// asks for "OpenAI の API キーを設定したい" and gets a card with a masked field; the key is typed there and posted to a dedicated endpoint, never written as a message. It is
// the same CredentialService.Save the settings screen and the MCP tool set_credential use. The provider of the cloud video models (set_video_provider, #1136) is the same
// modelsettings.SaveVideoProvider; choosing one that has no key yet is followed by the card that asks for it.
package apikey

import (
	"regexp"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/viaprovider"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/nlu"
)

// The operations of this package.
const (
	// OpSetCredential asks for the API key of a provider in a masked field. The key is not part of the operation.
	OpSetCredential commandinterpreter.OpType = "set_credential" //nolint:gosec // the name of an operation, not a credential
	// OpSetVideoProvider chooses the API provider the cloud video models (Kling) are called through.
	OpSetVideoProvider commandinterpreter.OpType = "set_video_provider"
)

// Args of both operations: the provider named by the director ("" when the message names none, and the chat asks which).
type Args struct {
	Provider string `json:"provider,omitempty"`
}

func init() {
	commandinterpreter.Register(OpSetCredential, commandinterpreter.Spec{
		Example: `{"type": "set_credential", "args": {"provider": "openai"}}`,
		Doc: `- "set_credential": the director wants to register or replace the API key of a service ("OpenAI の API キーを設定したい", "Claude のキーを入れ替えたい"). "args.provider" is one of ` + idList() + `; ` +
			`leave it out when the message names none. NEVER put the key itself anywhere in the answer: the chat shows a masked field for it.` + "\n",
		Clean:   cleanProvider(OpSetCredential, func(id string) bool { _, ok := labelOf(id); return ok }),
		Asked:   AsksKey,
		Detect:  detectKey,
		AnyMode: true, Confirm: true, Global: true, Setup: true,
	})
	commandinterpreter.Register(OpSetVideoProvider, commandinterpreter.Spec{
		Example: `{"type": "set_video_provider", "args": {"provider": "openrouter"}}`,
		Doc: `- "set_video_provider": the director wants the cloud video models (Kling) called through another API provider ("Kling は OpenRouter 経由で使いたい", "公式に戻して"). ` +
			`"args.provider" is "official" (each maker's own API), "openrouter", "fal" or "replicate". It does not set a key.` + "\n",
		Clean:   cleanProvider(OpSetVideoProvider, func(id string) bool { return viaprovider.Provider(id).Valid() }),
		Asked:   AsksVideoProvider,
		Detect:  detectVideoProvider,
		AnyMode: true, Confirm: true, Global: true, Setup: true,
	})
}

func idList() string {
	quoted := make([]string, 0, len(providers))
	for _, p := range providers {
		quoted = append(quoted, `"`+p.ID+`"`)
	}
	return strings.Join(quoted, ", ")
}

// cleanProvider keeps the provider the model named when it is one of the valid ones; a name it does not know is dropped (the chat then asks which), not guessed.
func cleanProvider(t commandinterpreter.OpType, valid func(string) bool) func(commandinterpreter.Operation) (commandinterpreter.Operation, bool) {
	return func(op commandinterpreter.Operation) (commandinterpreter.Operation, bool) {
		var a Args
		if err := op.DecodeArgs(&a); err != nil {
			return commandinterpreter.Operation{}, false
		}
		a.Provider = strings.ToLower(strings.TrimSpace(a.Provider))
		if !valid(a.Provider) {
			a.Provider = ""
		}
		out, err := commandinterpreter.NewOperation(t, a)
		return out, err == nil
	}
}

var (
	keyWord = regexp.MustCompile(`(?i)(api\s*キー|apiキー|api\s*key|アクセスキー|シークレットキー)`)
	weakKey = regexp.MustCompile(`キー(?:を|は|が|に)`)
	keyVerb = regexp.MustCompile(`(?i)(設定|登録|入力|入れ|いれ|変更|変え|置き換|差し替|追加|セット|更新|渡|set|register|enter|replace)`)
	// keyRequest is a key-entry verb in a form that asks for it, which a question mark does not make a question: "登録してくれる？", "更新してもらえますか？", "設定してもらっていいですか？",
	// "登録しておいて？" / "登録しといて？" (not 「しておいてって言った？」, 「しといて大丈夫？」), "入力できる？", "入れ替えたいんだけど？", "入れ直したいんだけど？", "設定してほしいんだけど？". A verb that is not one of these ("設定画面はどこ？", "教えて",
	// "設定するにはどうしたらいい？") stays a question.
	keyRequest = regexp.MustCompile(`(?:設定|登録|入力|入れ替え|入れ換え|入れ変え|入れ|いれ|変更|変え|置き換え|差し替え|追加|セット|更新|渡)(?:し)?(?:(?:直|なお)し)?(?:て(?:くれ|もらえ|もらいたい|もらっ(?:て|ても)(?:いい|よい|良い)|おいて(?:くれ|もらえ|もらいたい|ください|下さい|いただ|頂|ほしい|欲しい|[ねよ]?[？?！!。]*\s*$)|ください|下さい|いただ|頂|ほしい|欲しい)|といて(?:くれ|もらえ|もらいたい|ください|下さい|いただ|頂|ほしい|欲しい|[ねよ]?[？?！!。]*\s*$)|たい|(?:でき|られ)(?:る|ます|ない))`)
	// askWhere is a form that asks where, which, when, what or how ("どこで設定できますか？", "変更したいけどどこ？", "どうしたらいい？"): the message asks to be told, not to set a key.
	// 「いつでも」「どうしても」「どうぞ」「何度も」 are no question.
	askWhere = regexp.MustCompile(`どこ|どれ|どの|いつ(?:から|まで|の|に|なら|が|は|[？?])|何(?:を|が|で|の|と|に)|どう(?:やって|したら|すれば|すると|すれ|する)`)
	// askedPast ends a question about what was done ("設定してくれた？", "設定してくれたっけ", "設定してくれましたか？"): the 「くれ」 of keyRequest is past here, so it is no request.
	askedPast = regexp.MustCompile(`て(?:くれた|くれました|もらえた|もらえました|もらった|もらいました)(?:っけ|かな|の|か|かい)?\s*[？?]?\s*$`)
	videoWord = regexp.MustCompile(`(?i)(kling|クリング|動画モデル|クラウド動画|video model)`)
	viaWord   = regexp.MustCompile(`(?i)(経由|プロバイダー|provider|公式に|公式へ|使いたい|使って|切り替え|切替|変えて|変更|にして|に戻)`)
)

// AsksKey reports whether the message asks to set an API key: it names a key and does something to it. A message that only mentions the word ("API キーは持っている") is not, and neither
// is a question ("API キーの設定画面はどこ？", "OpenAI の API キーを教えて"); a request in the form of a question ("API キーを登録してくれる？") is one.
func AsksKey(message string) bool {
	if askedPast.MatchString(message) || (nlu.IsInquiry(message) && (!keyRequest.MatchString(message) || askWhere.MatchString(message))) || !keyVerb.MatchString(message) || asksVideoProvider(message) {
		return false
	}
	if keyWord.MatchString(message) {
		return true
	}
	// "Claude のキーを入れ替えたい": a bare キー counts only beside the name of a service (a ショートカットキー is no API key)
	_, named := providerOf(message)
	return named && weakKey.MatchString(message)
}

// AsksVideoProvider reports whether the message asks to call the cloud video models through another provider.
func AsksVideoProvider(message string) bool { return asksVideoProvider(message) }

func asksVideoProvider(message string) bool {
	if !videoWord.MatchString(message) || !viaWord.MatchString(message) {
		return false
	}
	_, named := videoProviderOf(message)
	return named
}

func detectKey(message string) []commandinterpreter.Operation {
	if !AsksKey(message) {
		return nil
	}
	var a Args
	if p, ok := providerOf(message); ok {
		a.Provider = p.ID
	}
	op, err := commandinterpreter.NewOperation(OpSetCredential, a)
	if err != nil {
		return nil
	}
	return []commandinterpreter.Operation{op}
}

func detectVideoProvider(message string) []commandinterpreter.Operation {
	if !asksVideoProvider(message) {
		return nil
	}
	p, _ := videoProviderOf(message)
	op, err := commandinterpreter.NewOperation(OpSetVideoProvider, Args{Provider: string(p)})
	if err != nil {
		return nil
	}
	return []commandinterpreter.Operation{op}
}

// videoProviderOf finds the API provider the message names: OpenRouter, fal.ai, Replicate, or "公式" (the makers' own).
func videoProviderOf(message string) (viaprovider.Provider, bool) {
	if p, ok := providerAmong(message, string(viaprovider.OpenRouter), string(viaprovider.Fal), string(viaprovider.Replicate)); ok {
		return viaprovider.Provider(p.ID), true
	}
	if strings.Contains(message, "公式") {
		return viaprovider.Official, true
	}
	return "", false
}
