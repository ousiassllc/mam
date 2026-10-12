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

func TestTextMessage_HasNoLinkWithoutADestinationOrFromAUser(t *testing.T) {
	assert.NotContains(t, render(t, molecules.TextMessage(textMessage(domain.RoleAssistant, "", ""))), `<a `)
	assert.NotContains(t, render(t, molecules.TextMessage(textMessage(domain.RoleAssistant, domain.RefExport, ""))), `<a `)
	assert.NotContains(t, render(t, molecules.TextMessage(textMessage(domain.RoleUser, domain.RefExport, "e1"))), `<a `)
}
