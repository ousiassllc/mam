package molecules_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
)

func textMessage(role domain.MessageRole, refType, refID string) domain.ThreadMessage {
	return domain.ThreadMessage{ChatMessage: domain.ChatMessage{
		ID: "m", ProjectID: "p1", Role: role, Kind: domain.KindText, Content: "通知", RefType: refType, RefID: refID,
	}}
}

func TestTextMessage_DrawsTheLinkOfAnExportNoticeFromItsRef(t *testing.T) {
	html := render(t, molecules.TextMessage(textMessage(domain.RoleAssistant, domain.RefExport, "e?1")))

	assert.Contains(t, html, `<a href="/projects/p1/exports/e%3F1/video"`)
	assert.Contains(t, html, `download`)
}

func TestTextMessage_DrawsTheSettingsLinkOfAStorageNotice(t *testing.T) {
	html := render(t, molecules.TextMessage(textMessage(domain.RoleAssistant, domain.RefSettings, "")))

	assert.Contains(t, html, `<a href="/projects/p1/settings"`)
}

func TestTextMessage_DrawsTheLinkOfACategoryOfTheGlobalSettingsFromItsSlug(t *testing.T) {
	for slug, want := range map[string][2]string{
		"api-keys":     {"/settings/api-keys", "API キーの設定を開く"},
		"local-models": {"/settings/local-models", "ローカル・代替モデルの設定を開く"},
		"update":       {"/settings/update", "バージョンとアップデートを開く"},
		"":             {"/settings", "設定画面を開く"},
		"nope":         {"/settings", "設定画面を開く"},
		"../projects":  {"/settings", "設定画面を開く"},
	} {
		html := render(t, molecules.TextMessage(textMessage(domain.RoleAssistant, domain.RefGlobalSettings, slug)))

		assert.Contains(t, html, `<a href="`+want[0]+`"`, slug)
		assert.Contains(t, html, want[1], slug)
	}
	assert.NotContains(t, render(t, molecules.TextMessage(textMessage(domain.RoleUser, domain.RefGlobalSettings, "api-keys"))), `<a `, "only a notice of the app has a link")
}

func TestTextMessage_HasNoLinkWithoutADestinationOrFromAUser(t *testing.T) {
	assert.NotContains(t, render(t, molecules.TextMessage(textMessage(domain.RoleAssistant, "", ""))), `<a `)
	assert.NotContains(t, render(t, molecules.TextMessage(textMessage(domain.RoleAssistant, domain.RefExport, ""))), `<a `)
	assert.NotContains(t, render(t, molecules.TextMessage(textMessage(domain.RoleUser, domain.RefExport, "e1"))), `<a `)
}
