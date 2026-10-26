package a11y_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/credentials"
)

// A button that repeats down a list names its row, so that a button list or a voice command tells the
// rows apart (#793). Each test renders two rows and expects two different names led by the visible text.

func TestTakeRow_JudgeAndRepairAreNamedAfterTheirTake(t *testing.T) {
	var html string
	for n, model := range []string{"kling", "veo"} {
		take := domain.Take{ID: "t", ShotID: "s1", ModelName: model, Status: domain.TakeCandidate, FilePath: "a.mp4"}
		html += render(t, molecules.TakeRow(take, n, false, true, true))
	}

	assert.Equal(t, []string{"Judge: Take A · kling", "Judge: Take B · veo"}, buttonNames(html, "Judge"))
	assert.Equal(t, []string{"Repair: Take A · kling", "Repair: Take B · veo"}, buttonNames(html, "Repair"))
}

func TestReviewComments_DeleteButtonsAreNamedAfterTheirComment(t *testing.T) {
	ms := int64(12000)
	html := render(t, organisms.ReviewComments(domain.ReviewInbox{ID: "r1", Comments: []domain.ReviewInboxComment{
		{ID: "c1", TimecodeMs: &ms, Body: "色が暗い"},
		{ID: "c2", Body: "全体に長い"},
	}}))

	assert.Equal(t, []string{"削除: 0:12 色が暗い", "削除: 全体 全体に長い"}, buttonNames(html, "削除"))
}

func TestReferenceLibrary_RemoveButtonsAreNamedAfterTheirImage(t *testing.T) {
	html := render(t, organisms.ReferenceLibrary(organisms.ReferenceLibraryData{
		Project: domain.Project{ID: "p1"},
		Items: []domain.ReferenceItem{
			{ID: "i1", Kind: domain.LibraryKinds()[0], Name: "主人公", Source: domain.SourceUploaded},
			{ID: "i2", Kind: domain.LibraryKinds()[0], Name: "背景", Source: domain.SourceUploaded},
		},
	}))

	assert.Equal(t, []string{"ライブラリから外す: 主人公", "ライブラリから外す: 背景"}, buttonNames(html, "ライブラリから外す"))
}

func TestVersionHistoryPanel_RestoreButtonsAreNamedAfterTheirVersion(t *testing.T) {
	now := time.Now()
	html := render(t, organisms.VersionHistoryPanel(organisms.VersionHistoryData{
		DOMID: "vh", Title: "履歴", BasePath: "/x",
		Entries: []domain.VersionEntry{{Number: 3, CreatedAt: now}, {Number: 2, CreatedAt: now}, {Number: 1, CreatedAt: now}},
	}))

	assert.Equal(t, []string{"このバージョンに復元: v2", "このバージョンに復元: v1"}, buttonNames(html, "このバージョンに復元"))
}

func TestCredentialPanel_OpenAndDeleteButtonsAreNamedAfterTheirProvider(t *testing.T) {
	html := render(t, credentials.Panel(credentials.Data{Providers: []credentials.ProviderView{
		{Provider: "kling", Label: "Kling", Configured: true},
		{Provider: "veo", Label: "Veo", Configured: true},
		{Provider: "runway", Label: "Runway"},
	}}))

	assert.Equal(t, []string{"変更: Kling", "変更: Veo"}, buttonNames(html, "変更"))
	assert.Equal(t, []string{"設定: Runway"}, buttonNames(html, "設定"))
	assert.Equal(t, []string{"削除: Kling", "削除: Veo"}, buttonNames(html, "削除"))
}

// The buttons of the three login cards are named after their card: "ログアウト" and "利用できるモデルを表示" repeat.
func TestChatGPTCards_ButtonsAreNamedAfterTheirLoginSystem(t *testing.T) {
	html := render(t, chatgpt.Panel(chatgpt.Data{
		Official:     chatgpt.Account{SignedIn: true, PlanUsageGranted: true},
		Image:        chatgpt.Account{SignedIn: true},
		ImageEnabled: true,
		Codex:        chatgpt.Codex{SignedIn: true, Engine: chatgpt.CodexEngine{State: "ready", Supported: true}},
	}))

	assert.Equal(t, []string{
		"ログアウト: Sign in with ChatGPT（公式・テキスト生成）",
		"ログアウト: ChatGPT 画像生成（非公式）",
		"ログアウト: ChatGPT 経由の Codex（テキスト生成のみ）",
	}, buttonNames(html, "ログアウト"))
	assert.Equal(t, []string{
		"利用できるモデルを表示: Sign in with ChatGPT（公式・テキスト生成）",
		"利用できるモデルを表示: ChatGPT 経由の Codex（テキスト生成のみ）",
	}, buttonNames(html, "利用できるモデルを表示"))
}

func TestNoteCard_ButtonsAreNamedAfterTheirNote(t *testing.T) {
	archived := time.Now()
	card := func(id, title string, archivedAt *time.Time) domain.NoteView {
		return domain.NoteView{Note: domain.Note{ID: id, ArchivedAt: archivedAt}, Version: domain.NoteVersion{Number: 1, Title: title}}
	}
	html := render(t, molecules.NoteCard(card("n1", "世界観", nil))) +
		render(t, molecules.NoteCard(card("n2", "口調", nil))) +
		render(t, molecules.NoteCard(card("n3", "禁止事項", &archived)))

	assert.Equal(t, []string{"保存: 世界観", "保存: 口調", "保存: 禁止事項"}, buttonNames(html, "保存"))
	assert.Equal(t, []string{"アーカイブ: 世界観", "アーカイブ: 口調"}, buttonNames(html, "アーカイブ"))
	assert.Equal(t, []string{"アーカイブを解除: 禁止事項"}, buttonNames(html, "アーカイブを解除"))
}

func TestReplyButton_IsNamedAfterItsCard(t *testing.T) {
	var html string
	for _, text := range []string{"どちらにしますか", "予算を超えます"} {
		html += render(t, molecules.ReplyButton(domain.ThreadMessage{ChatMessage: domain.ChatMessage{
			ID: text, Kind: domain.KindApproval, Content: text,
		}}))
	}

	assert.Equal(t, []string{"返信: どちらにしますか", "返信: 予算を超えます"}, buttonNames(html, "返信"))
}
