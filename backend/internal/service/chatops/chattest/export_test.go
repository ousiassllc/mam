package chattest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/chatfix"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/export"
	"github.com/ousiassllc/mam/backend/internal/service/chatwire"
)

func TestChat_ExportAsksFirstAndRendersOnlyAfterTheAnswer(t *testing.T) {
	f, fake := newExportFixture(t)

	ws := f.Say("この動画を縦長の720pで書き出して", "")

	cards := chatfix.Cards(ws.Messages)
	require.Len(t, cards, 1)
	body := chatfix.Body(cards[0])
	assert.Contains(t, body.Text, "9:16（既定: 16:9）")
	assert.Contains(t, body.Text, "720p（既定: 1080p）")
	assert.Contains(t, body.Text, "ffmpeg で書き出します（時間がかかります")
	assert.NotContains(t, body.Text, "全プロジェクト", "an export belongs to this Project")
	assert.Empty(t, fake.called, "nothing is rendered before the answer")

	ws = f.Answer(cards[0].ID, body.Options[0])

	assert.Equal(t, []export.Args{{AspectRatio: "9:16", Resolution: "720p", Codec: "h264"}}, fake.called, "exported once, with the options of the card")
	assert.Contains(t, chatfix.LastText(ws), "書き出しが完了しました（9:16 / 720p / h264）")
	assert.Contains(t, chatfix.LastText(ws), "/projects/"+f.ProjectID+"/exports/e-new/video")
}

func TestChat_DecliningOrIgnoringTheExportCardRendersNothing(t *testing.T) {
	f, fake := newExportFixture(t)
	cards := chatfix.Cards(f.Say("この動画を書き出して", "").Messages)
	require.Len(t, cards, 1)

	ws := f.Answer(cards[0].ID, "やめる")
	assert.Contains(t, chatfix.LastText(ws), "何も変更していません")

	cards = chatfix.Cards(f.Say("この動画を書き出して", "").Messages)
	require.NotEmpty(t, cards)
	f.Answer(cards[len(cards)-1].ID, "もう少し考えます")
	assert.Empty(t, fake.called, "an unrelated reply is not a yes")
}

func TestChat_ExportAnAlreadyExportedProjectIsAskedAgain(t *testing.T) {
	f, fake := newExportFixture(t)
	fake.state = domain.StateExported

	cards := chatfix.Cards(f.Say("この動画をもう一度書き出して", "").Messages)

	require.Len(t, cards, 1)
	assert.Contains(t, chatfix.Body(cards[0]).Text, "もう一度書き出します")
}

// The state gate is the real one of the editor: a Project that is not READY / EXPORTED gets no card, only the reason.
func TestChat_ExportOfAProjectThatIsNotReadyPostsNoCardAndSaysWhy(t *testing.T) {
	f := chatfix.New(t, chatfix.Wired(chatwire.Settings{}))
	f.SQL(`UPDATE project SET state = 'AUDIO' WHERE id = ?`, f.ProjectID)

	ws := f.Say("この動画を書き出して", "")

	assert.Empty(t, chatfix.Cards(ws.Messages))
	text := chatfix.LastText(ws)
	assert.Contains(t, text, "現在の状態: AUDIO")
	assert.Contains(t, text, "Final QC に合格")
	assert.Equal(t, 0, countRows(f, `SELECT COUNT(*) FROM export`))
}

// With no ffmpeg (this fixture has none) the real editor says so, as the screen does, and no card is posted.
func TestChat_ExportWithoutFFmpegPostsNoCard(t *testing.T) {
	f := chatfix.New(t, chatfix.Wired(chatwire.Settings{}))
	f.SQL(`UPDATE project SET state = 'READY' WHERE id = ?`, f.ProjectID)

	ws := f.Say("この動画を書き出して", "")

	assert.Empty(t, chatfix.Cards(ws.Messages))
	assert.Contains(t, chatfix.LastText(ws), "「モデルマネージャー」の「付随ソフト」")
	assert.Equal(t, "READY", projectState(f))
}

func TestChat_ExportTheModelsWordAloneIsNotEnough(t *testing.T) {
	f, fake := newExportFixture(t)

	ws := f.SayOps("カメラを固定して", `{"type":"export_video"}`, `{"type":"get_export_download_url"}`, `{"type":"export_project_bundle"}`)

	assert.Empty(t, chatfix.Cards(ws.Messages))
	assert.NotContains(t, chatfix.LastText(ws), "/bundle")
	assert.NotContains(t, chatfix.LastText(ws), "ダウンロード")
	assert.Empty(t, fake.called)
}

func TestChat_TheModelsExportOperationAsksTheSameCardAsTheWords(t *testing.T) {
	f, fake := newExportFixture(t)

	cards := chatfix.Cards(f.SayOps("この動画を書き出して", `{"type":"export_video","args":{"aspect_ratio":"1：1","resolution":"1080","codec":"HEVC"}}`).Messages)

	require.Len(t, cards, 1)
	assert.Contains(t, chatfix.Body(cards[0]).Text, "1:1（既定: 16:9）")
	assert.Contains(t, chatfix.Body(cards[0]).Text, "h265（既定: h264）")
	assert.Empty(t, fake.called)
}

func TestChat_DownloadAnswersAtOnceWithTheLatestExportAndChangesNothing(t *testing.T) {
	f, fake := newExportFixture(t)
	insertExport(f, "e1", "16:9", "1080p")
	insertExport(f, "e2", "9:16", "720p")

	ws := f.Say("さっきの書き出しをダウンロードしたい", "")

	assert.Empty(t, chatfix.Cards(ws.Messages), "nothing changes, so nothing is asked")
	var notice *domain.ThreadMessage
	for i := range ws.Messages {
		if ws.Messages[i].RefType == domain.RefExport {
			notice = &ws.Messages[i]
		}
	}
	require.NotNil(t, notice, "the chat draws the download button of the latest export")
	assert.Equal(t, "e2", notice.RefID)
	assert.Contains(t, notice.Content, "9:16 / 720p")
	assert.Empty(t, fake.called)
	assert.Equal(t, "READY", projectState(f))
}

func TestChat_DownloadWithoutAnExportSaysSoAndOffersToExport(t *testing.T) {
	f, fake := newExportFixture(t)

	ws := f.Say("書き出した動画をダウンロードして", "")

	assert.Contains(t, chatfix.LastText(ws), "まだ書き出した動画がありません")
	assert.Contains(t, chatfix.LastText(ws), "書き出して")
	assert.Empty(t, chatfix.Cards(ws.Messages))
	assert.Empty(t, fake.called)
}

func TestChat_TheBundleLinkIsTheAppsRoute(t *testing.T) {
	f, _ := newExportFixture(t)

	ws := f.Say("プロジェクトをバンドルで書き出したい", "")

	assert.Empty(t, chatfix.Cards(ws.Messages), "the bundle is not an export of the video, and nothing changes")
	assert.Contains(t, chatfix.LastText(ws), "/projects/"+f.ProjectID+"/bundle")
}

// No LLM is set up: what the message plainly asks for is still done.
func TestChat_ExportRequestsAreReadWithoutAModel(t *testing.T) {
	f := chatfix.New(t, chatfix.WithoutLLM(), chatfix.Wired(chatwire.Settings{}))
	f.SQL(`UPDATE project SET state = 'READY' WHERE id = ?`, f.ProjectID)

	ws := f.Say("さっきの書き出しをダウンロードしたい", "")
	assert.Contains(t, chatfix.LastText(ws), "まだ書き出した動画がありません")

	ws = f.Say("プロジェクトをバンドルで書き出したい", "")
	assert.Contains(t, chatfix.LastText(ws), "/projects/"+f.ProjectID+"/bundle")

	ws = f.Say("この動画を書き出して", "")
	assert.Empty(t, chatfix.Cards(ws.Messages))
	assert.Contains(t, chatfix.LastText(ws), "「モデルマネージャー」の「付随ソフト」")
}
