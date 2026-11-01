package localvoice

import (
	"errors"
	"fmt"
	"strings"
)

// The sentences for the user of a local provider that cannot be used. They are defined here, once, because the Audio Director (which
// writes the Skipped entry of a lip sync), the service (the error of a run, the notice of a switch to the cloud, the state on the
// settings screen) and the MCP tools all say the same words, and this package is the one that both the agent layer and the service
// layer may import. A caller only chooses which error to put in; it never writes a sentence of its own.

// words are the names a provider goes by in a sentence.
type words struct {
	// local is the local provider, cloud the one to use instead, and setting what the user chooses in the settings screen.
	local, cloud, setting string
}

// kindText is a reason in two parts around the Detail: head says what is wrong, tail what to do about it.
type kindText struct{ head, tail string }

const managerSection = "モデルマネージャーの「ローカル音声・口パク」"

func switchTail(w words) string {
	return fmt.Sprintf("%sを使うには、設定の「音声・口パク」で%s を選ぶか、「ローカルが使えないときはクラウドに切り替える」をオンにしてください", w.cloud, w.setting)
}

var voiceWords = words{local: "ローカルの声（GPT-SoVITS）", cloud: "クラウドの声（ElevenLabs）", setting: "声のプロバイダーに ElevenLabs"}

var lipSyncWords = words{local: "ローカルの口パク（MuseTalk 1.5）", cloud: "クラウドの口パク（Sync Labs）", setting: "口パクのプロバイダーに Sync Labs"}

func (w words) text(kind Kind) kindText {
	sw := switchTail(w)
	switch kind {
	case KindNotInstalled:
		return kindText{w.local + "が導入されていません", managerSection + "から導入してください。" + sw}
	case KindWeightsMissing:
		return kindText{w.local + "の重みが取得されていません", managerSection + "で「重みを導入」を実行してください。" + sw}
	case KindUnsupported:
		return kindText{"この PC では" + w.local + "を使えません", sw}
	case KindGPUMemory:
		return kindText{w.local + "に GPU のメモリが足りません", "他のアプリ（ゲーム・ブラウザ・画像や動画の生成）を閉じて GPU のメモリを空けると使えることがあります。" + sw}
	case KindProcess:
		return kindText{w.local + "が動きませんでした", managerSection + "のカードで、ログの末尾と導入先を確認してください。" + sw}
	case KindTimeout:
		return kindText{w.local + "が時間内に終わりませんでした", "もう一度試すか、PC が忙しくないときに試してください。" + sw}
	case KindNoFace:
		return kindText{"動画から顔を見つけられなかったので、口パクを作れませんでした", ""}
	case KindTooLarge:
		return kindText{"動画が大きすぎて、" + w.local + "で扱えませんでした", "動画を短くしてください。" + sw}
	}
	return kindText{w.local + "を使えません", sw}
}

// Reason is the sentence for the user of a local provider that cannot be used: what is wrong, the Detail the runtime or the server
// gave (in parentheses, as it is) and what to do about it, which names where to fix it (the model manager) and how to use the cloud
// provider instead. A provider that has a reason of its own for a clip (no face in the video) says only that.
func Reason(un *UnavailableError) string {
	w := voiceWords
	if un.Vendor == VendorMuseTalk {
		w = lipSyncWords
	}
	t := w.text(un.Kind)
	out := t.head
	if un.Detail != "" {
		out += "（" + strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(un.Detail), "。")) + "）"
	}
	if t.tail != "" {
		out += "。" + t.tail
	}
	return out
}

// Role is what a provider does in a run: "voice" or "lip_sync".
type Role string

// The two roles a run chooses a provider for.
const (
	RoleVoice   Role = "voice"
	RoleLipSync Role = "lip_sync"
)

// CloudName is the sentence's name of the cloud provider that takes the place of the local one ("クラウドの声（ElevenLabs）").
func (p Role) CloudName() string {
	if p == RoleLipSync {
		return lipSyncWords.cloud
	}
	return voiceWords.cloud
}

// CloudKeyID is the credential of the cloud provider of p ("elevenlabs", "synclabs").
func (p Role) CloudKeyID() string {
	if p == RoleLipSync {
		return "synclabs"
	}
	return "elevenlabs"
}

// LocalName is the sentence's name of the local provider of p.
func (p Role) LocalName() string {
	if p == RoleLipSync {
		return lipSyncWords.local
	}
	return voiceWords.local
}

// NoCloudKeyError is a run that must go to the cloud because its local provider cannot be used, but the cloud provider's API key is
// not stored: nothing is paid for and nothing is started. Cause is the sentence of why the local one cannot be used.
type NoCloudKeyError struct {
	Role  Role
	Cause string
}

func (e *NoCloudKeyError) Error() string {
	return fmt.Sprintf("%sを使えないため、%sに切り替えようとしましたが、その API キーが設定されていません。設定の「API キーとログイン」で %s のキーを登録するか、"+
		"%sを使えるようにしてください。使えない理由: %s", e.Role.LocalName(), e.Role.CloudName(), e.Role.CloudKeyID(), e.Role.LocalName(), e.Cause)
}

// Switched is the notice of a provider the run changed to the cloud: it says which, and why.
func Switched(p Role, cause string) string {
	return fmt.Sprintf("%sを使えなかったため、この実行は%sで作りました。使えない理由: %s", p.LocalName(), p.CloudName(), cause)
}

// UnavailableReason is Reason of the *UnavailableError inside err, and false for any other error.
func UnavailableReason(err error) (string, bool) {
	var un *UnavailableError
	if !errors.As(err, &un) {
		return "", false
	}
	return Reason(un), true
}
