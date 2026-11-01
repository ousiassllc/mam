package localvoice_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
)

var everyKind = []localvoice.Kind{
	localvoice.KindNotInstalled, localvoice.KindWeightsMissing, localvoice.KindUnsupported, localvoice.KindGPUMemory,
	localvoice.KindProcess, localvoice.KindTimeout, localvoice.KindNoFace, localvoice.KindTooLarge,
}

// Every kind has a sentence of its own for the voice and for the lip sync, each naming its own model, so that a screen, the chat, the MCP
// tools and a failed job all say the same thing.
func TestReason_EveryKindHasItsOwnSentenceForEachProvider(t *testing.T) {
	for _, vendor := range []string{"", localvoice.VendorMuseTalk} {
		seen := map[string]localvoice.Kind{}
		for _, kind := range everyKind {
			text := localvoice.Reason(&localvoice.UnavailableError{Kind: kind, Vendor: vendor, Detail: "理由の詳細"})
			assert.Contains(t, text, "（理由の詳細）", "%s: the Detail is in parentheses, as it is", kind)
			other, dup := seen[text]
			assert.False(t, dup, "%s has the sentence of %s", kind, other)
			seen[text] = kind
		}
	}
	voice := localvoice.Reason(&localvoice.UnavailableError{Kind: localvoice.KindProcess})
	lip := localvoice.Reason(&localvoice.UnavailableError{Kind: localvoice.KindProcess, Vendor: localvoice.VendorMuseTalk})
	assert.Contains(t, voice, "GPT-SoVITS")
	assert.NotContains(t, voice, "MuseTalk")
	assert.Contains(t, lip, "MuseTalk")
	assert.NotContains(t, lip, "GPT-SoVITS")
}

// What to do is in the sentence: where to fix it (the model manager), and how to use the cloud instead.
func TestReason_SaysWhatToDo(t *testing.T) {
	notInstalled := localvoice.Reason(&localvoice.UnavailableError{Kind: localvoice.KindNotInstalled})
	assert.Contains(t, notInstalled, "ローカルの声（GPT-SoVITS）が導入されていません")
	assert.Contains(t, notInstalled, "モデルマネージャーの「ローカル音声・口パク」から導入してください")

	weights := localvoice.Reason(&localvoice.UnavailableError{Kind: localvoice.KindWeightsMissing, Detail: "足りない重み・依存ファイル: s2G.pth"})
	assert.Contains(t, weights, "（足りない重み・依存ファイル: s2G.pth）", "the missing asset is named")
	assert.Contains(t, weights, "「重みを導入」を実行してください")

	macOS := localvoice.Reason(&localvoice.UnavailableError{Kind: localvoice.KindUnsupported, Vendor: localvoice.VendorMuseTalk, Detail: "macOS では公式の PyTorch 版が使えません。"})
	assert.Contains(t, macOS, "この PC ではローカルの口パク（MuseTalk 1.5）を使えません（macOS では公式の PyTorch 版が使えません）", "the reason of the runtime, once, without its full stop")
	assert.Contains(t, macOS, "クラウドの口パク（Sync Labs）を使うには、設定の「音声・口パク」で口パクのプロバイダーに Sync Labs を選ぶか")

	gpu := localvoice.Reason(&localvoice.UnavailableError{Kind: localvoice.KindGPUMemory, Detail: "RTX 3050 のビデオメモリは 2.0 GB で、必要な 4 GB に足りません"})
	assert.Contains(t, gpu, "GPU のメモリが足りません（RTX 3050 のビデオメモリは 2.0 GB で、必要な 4 GB に足りません）")
	assert.Contains(t, gpu, "GPU のメモリを空けると", "something the user can do now")
	assert.Contains(t, gpu, "クラウドの声（ElevenLabs）を使うには")

	process := localvoice.Reason(&localvoice.UnavailableError{Kind: localvoice.KindProcess})
	assert.Contains(t, process, "ログの末尾と導入先を確認してください")
}

// A clip without a face is a fact of the clip: the sentence says that and sends the user nowhere.
func TestReason_NoFaceIsAboutTheClipNotTheRuntime(t *testing.T) {
	text := localvoice.Reason(&localvoice.UnavailableError{Kind: localvoice.KindNoFace, Vendor: localvoice.VendorMuseTalk})

	assert.Equal(t, "動画から顔を見つけられなかったので、口パクを作れませんでした", text)
}

func TestUnavailableReason_OnlyForAnUnavailableError(t *testing.T) {
	text, ok := localvoice.UnavailableReason(fmt.Errorf("line 1: %w", &localvoice.UnavailableError{Kind: localvoice.KindNotInstalled}))
	assert.True(t, ok)
	assert.Contains(t, text, "導入されていません")

	_, ok = localvoice.UnavailableReason(errors.New("boom"))
	assert.False(t, ok)
}

func TestNoCloudKeyError_TellsWhereToStoreTheKeyAndWhyTheLocalOneCannotBeUsed(t *testing.T) {
	voice := &localvoice.NoCloudKeyError{Role: localvoice.RoleVoice, Cause: "ローカルの声（GPT-SoVITS）が導入されていません"}
	lip := &localvoice.NoCloudKeyError{Role: localvoice.RoleLipSync, Cause: "x"}

	assert.Contains(t, voice.Error(), "クラウドの声（ElevenLabs）に切り替えようとしましたが、その API キーが設定されていません")
	assert.Contains(t, voice.Error(), "設定の「API キーとログイン」で elevenlabs のキーを登録")
	assert.Contains(t, voice.Error(), "使えない理由: ローカルの声（GPT-SoVITS）が導入されていません")
	assert.Contains(t, lip.Error(), "synclabs のキーを登録")
	assert.Contains(t, lip.Error(), "クラウドの口パク（Sync Labs）")
}

func TestSwitched_SaysWhichProviderTheRunWentToAndWhy(t *testing.T) {
	assert.Equal(t, "ローカルの声（GPT-SoVITS）を使えなかったため、この実行はクラウドの声（ElevenLabs）で作りました。使えない理由: 理由", localvoice.Switched(localvoice.RoleVoice, "理由"))
	assert.Contains(t, localvoice.Switched(localvoice.RoleLipSync, "理由"), "クラウドの口パク（Sync Labs）で作りました")
}
