package chattest_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/chatfix"
	"github.com/ousiassllc/mam/backend/internal/service/chatwire"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

// realInstallFixture wires the install operations of the chat to a real model manager over fakes (mmtest: a fake host for the engine archive and the model files), as the app
// does: the same modelmanager.Service the MCP tools use.
func realInstallFixture(t *testing.T, opts ...mmtest.Option) (*chatfix.Fixture, *mmtest.Rig) {
	t.Helper()
	f := chatfix.New(t)
	rig := mmtest.New(t, append([]mmtest.Option{mmtest.WithDB(f.DB)}, opts...)...)
	f.Svc.ModelManager = rig.Svc
	chatwire.Connect(f.Svc, chatwire.Settings{})
	f.Direct()
	return f, rig
}

func installingProgress(f *chatfix.Fixture) domain.ThreadMessage {
	f.T.Helper()
	var last domain.ThreadMessage
	for _, m := range f.Thread() {
		if m.Kind == domain.KindProgress {
			last = m
		}
	}
	require.Equal(f.T, domain.KindProgress, last.Kind, "a progress card in the thread")
	return last
}

// The whole way with the real model manager: no engine yet (refused, no card) -> the engine card -> accepted: a job and a progress card with how far it is -> then a model of
// the catalog: the card, accept, installed; asked again it is "nothing to do".
func TestChat_InstallEngineThenAModelWithTheRealModelManager(t *testing.T) {
	f, rig := realInstallFixture(t)

	ws := f.SayOps("A のモデルを入れて", `{"type":"install_model","args":{"model":"A"}}`)
	assert.Empty(t, chatfix.Cards(ws.Messages), "no card for something that would be refused")
	assert.Contains(t, chatfix.LastText(ws), "先に ComfyUI エンジンを導入してください")

	ws = f.SayOps("ComfyUI のエンジンを入れて", `{"type":"install_model_engine","args":{}}`)
	cards := chatfix.Cards(ws.Messages)
	require.Len(t, cards, 1)
	body := chatfix.Body(cards[0])
	for _, want := range []string{"ComfyUI エンジン", "NVIDIA", "展開後", "全プロジェクト", "GPL-3.0"} {
		assert.Contains(t, body.Text, want)
	}
	assert.False(t, rig.Svc.Engine().Installed, "nothing is installed before the user accepts")
	assert.Empty(t, rig.Svc.Engine().InstallJob)

	ws = f.Answer(cards[0].ID, body.Options[0])
	progress := installingProgress(f)
	assert.Contains(t, chatfix.LastText(ws), "ComfyUI エンジン")
	assert.Equal(t, rig.Svc.Engine().InstallJob, progress.RefID, "the card follows the job the model manager queued")
	assert.NotEmpty(t, progress.Detail, "the line under the card says how far it is")
	require.Equal(t, domain.JobSucceeded, rig.Wait(progress.RefID).State)
	assert.True(t, rig.Svc.Engine().Installed)
	assert.Equal(t, "導入が完了しました", installingProgress(f).Detail)

	card := chatfix.Cards(f.SayOps("A のモデルを入れて", `{"type":"install_model","args":{"model":"A"}}`).Messages)[0]
	body = chatfix.Body(card)
	for _, want := range []string{"「A」", "apache-2.0", "商用OK", "全プロジェクト"} {
		assert.Contains(t, body.Text, want)
	}
	f.Answer(card.ID, body.Options[0])
	progress = installingProgress(f)
	require.Equal(t, domain.JobSucceeded, rig.Wait(progress.RefID).State)
	m, err := rig.Svc.Model("m-a")
	require.NoError(t, err)
	assert.Equal(t, "installed", string(m.State))

	ws = f.SayOps("m-a のモデルを入れて", `{"type":"install_model","args":{"model":"m-a"}}`)
	assert.Empty(t, chatfix.Cards(ws.Messages))
	assert.Contains(t, chatfix.LastText(ws), "すでにその内容です")
	_, err = f.Svc.Chat.PostMessage(context.Background(), f.ProjectID, service.PostMessageInput{Content: "はい", ReplyToMessageID: card.ID})
	assert.ErrorIs(t, err, service.ErrConflict)
}

// The consent of the license is the button, and the model manager records it as the user's acceptance (the same LicenseRef as install_model with an acceptance); the adult
// model is neither found by name nor installable by id before the age confirmation.
func TestChat_InstallConsentAndAdultGateWithTheRealModelManager(t *testing.T) {
	f, rig := realInstallFixture(t)
	rig.InstallEngine()

	card := chatfix.Cards(f.SayOps("C のモデルを入れて", `{"type":"install_model","args":{"model":"C"}}`).Messages)[0]
	body := chatfix.Body(card)
	for _, want := range []string{"non-commercial", "版 1", "https://example.com/non-commercial", "商用NG", "EU、KR", "同意して導入する"} {
		assert.Contains(t, body.Text, want)
	}
	require.Equal(t, "同意して導入する", body.Options[0])
	before, err := rig.Svc.Model("m-c")
	require.NoError(t, err)
	assert.False(t, before.Accepted)

	f.Answer(card.ID, body.Options[0])

	after, err := rig.Svc.Model("m-c")
	require.NoError(t, err)
	assert.True(t, after.Accepted, "the button is recorded as the acceptance of the license shown")
	require.Equal(t, domain.JobSucceeded, rig.Wait(installingProgress(f).RefID).State)

	ws := f.SayOps("D のモデルを入れて", `{"type":"install_model","args":{"model":"D"}}`)
	assert.Empty(t, chatfix.Cards(ws.Messages))
	assert.Contains(t, chatfix.LastText(ws), "カタログに「D」というモデルはありません")
	ws = f.SayOps("m-d のモデルを入れて", `{"type":"install_model","args":{"model":"m-d"}}`)
	assert.Empty(t, chatfix.Cards(ws.Messages))
	assert.Contains(t, chatfix.LastText(ws), "年齢の確認")

	rig.ConfirmAdult()
	cards := chatfix.Cards(f.SayOps("D のモデルを入れて", `{"type":"install_model","args":{"model":"D"}}`).Messages)
	require.Len(t, cards, 1, "offered once the age is confirmed")
}

// An engine that is installed (an older pinned version) is updated through the chat with the real model manager: the card names the build the GPU recommends, as the
// install does (an installed engine has no recommendation of its own, which once made every update a "no GPU" refusal).
func TestChat_UpdateTheInstalledEngineWithTheRealModelManager(t *testing.T) {
	old := mmtest.New(t, mmtest.EngineVersion("v0.9.0"))
	old.InstallEngine()
	f, rig := realInstallFixture(t, mmtest.WithLayout(old.Layout))
	require.True(t, rig.Svc.Engine().UpdateAvailable, "the catalog pins v1.0.0 and v0.9.0 is installed")

	ws := f.SayOps("ComfyUI のエンジンを入れ直して", `{"type":"install_model_engine","args":{}}`)

	cards := chatfix.Cards(ws.Messages)
	require.Len(t, cards, 1, "a card, not a refusal: %s", chatfix.LastText(ws))
	body := chatfix.Body(cards[0])
	assert.Contains(t, body.Text, "ComfyUI エンジンを更新しますね？（導入済み v0.9.0 → v1.0.0）")
	assert.Contains(t, body.Text, "NVIDIA")
	assert.NotContains(t, chatfix.LastText(ws), "GPU が見つかりません")

	f.Answer(cards[0].ID, body.Options[0])
	require.Equal(t, domain.JobSucceeded, rig.Wait(rig.Svc.Engine().InstallJob).State)
	assert.False(t, rig.Svc.Engine().UpdateAvailable)
	assert.Equal(t, "v1.0.0", rig.Svc.Engine().Version)
}

// A machine with no supported GPU is refused with the reason also when the engine is installed and an update is pending: nothing is queued.
func TestChat_UpdateTheEngineOnAMachineWithoutAGPUIsRefusedWithTheReason(t *testing.T) {
	old := mmtest.New(t, mmtest.EngineVersion("v0.9.0"))
	old.InstallEngine()
	f, rig := realInstallFixture(t, mmtest.WithLayout(old.Layout))
	rig.GPUReport.Recommendation = gpu.Recommendation{Guidance: "対応する GPU が見つかりません（テスト）"}

	ws := f.SayOps("ComfyUI のエンジンを入れ直して", `{"type":"install_model_engine","args":{}}`)

	assert.Empty(t, chatfix.Cards(ws.Messages))
	assert.Contains(t, chatfix.LastText(ws), "対応する GPU が見つかりません（テスト）")
	assert.Empty(t, rig.Svc.Engine().InstallJob)
}

// An engine whose record says installed but whose files are gone (deleted by hand: Broken) is installed again through the chat with the real model manager: the card says the
// files are missing and names the build the GPU recommends, and accepting reinstalls it.
func TestChat_ReinstallTheBrokenEngineWithTheRealModelManager(t *testing.T) {
	old := mmtest.New(t)
	old.InstallEngine()
	require.NoError(t, os.RemoveAll(old.Layout.EngineDir()))
	f, rig := realInstallFixture(t, mmtest.WithLayout(old.Layout))
	require.True(t, rig.Svc.Engine().Broken, "the record is there and the files are not")
	require.False(t, rig.Svc.Engine().Installed)

	ws := f.SayOps("ComfyUI のエンジンを入れ直して", `{"type":"install_model_engine","args":{}}`)

	cards := chatfix.Cards(ws.Messages)
	require.Len(t, cards, 1, "a card, not a refusal or \"nothing to do\": %s", chatfix.LastText(ws))
	body := chatfix.Body(cards[0])
	assert.Contains(t, body.Text, "ComfyUI エンジンを入れ直しますね？（導入の記録はありますが、ファイルがありません）")
	assert.Contains(t, body.Text, "NVIDIA")
	assert.NotContains(t, chatfix.LastText(ws), "GPU が見つかりません")

	f.Answer(cards[0].ID, body.Options[0])
	require.Equal(t, domain.JobSucceeded, rig.Wait(rig.Svc.Engine().InstallJob).State)
	assert.True(t, rig.Svc.Engine().Installed)
	assert.False(t, rig.Svc.Engine().Broken)
}
