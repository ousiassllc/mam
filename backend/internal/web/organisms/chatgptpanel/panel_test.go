package chatgptpanel_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	view "github.com/ousiassllc/mam/backend/internal/web/organisms/chatgptpanel"
)

func render(t *testing.T, d view.Data) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, view.Panel(d).Render(context.Background(), &sb))
	return sb.String()
}

func TestPanel_AnOfficialLoginTheOrganizationRefusedPointsToTheCodexLoginAndSignsNothingOut(t *testing.T) {
	d := view.Data{Method: view.MethodOfficial}
	assert.NotContains(t, render(t, d), `data-testid="chatgpt-use-codex"`)

	d.Official.Login = &view.Login{Failed: true, UseCodex: true, Message: "refused"}
	html := render(t, d)
	assert.Contains(t, html, `data-testid="chatgpt-use-codex-button"`)
	assert.Contains(t, html, "/settings/chatgpt?method=codex")
	assert.NotContains(t, html, `data-testid="chatgpt-logout-official"`)

	d = view.Data{Method: view.MethodOfficial, Official: view.Account{SignedIn: true, WorkspacePlan: true, Plan: "enterprise"}}
	assert.Contains(t, render(t, d), `data-testid="chatgpt-use-codex"`)
}

func TestPanel_TheCodexBlockHasTheTermsTheEngineAndThePollingKeepsTheMethod(t *testing.T) {
	d := view.Data{Method: view.MethodCodex}
	d.Codex.Engine = view.CodexEngine{State: "installing", Supported: true, Install: view.CodexInstall{Running: true, Percent: 37, Message: "ダウンロード中"}}
	html := render(t, d)
	assert.Contains(t, html, "商用・ホスティング")
	assert.Contains(t, html, `hx-get="/settings/chatgpt?method=codex"`, "the install progress polls and stays on the Codex block")
	assert.Contains(t, html, "37%")
	assert.Contains(t, html, `data-testid="chatgpt-codex-install-cancel"`)
	assert.NotContains(t, html, `data-testid="chatgpt-codex-install-button"`)

	d.Codex.Engine = view.CodexEngine{State: "not_installed", Supported: true}
	assert.Contains(t, render(t, d), `data-testid="chatgpt-codex-install-button"`)
}

func TestPanel_TheUsageLimitResetsAreShownInJapanStandardTime(t *testing.T) {
	d := view.Data{Method: view.MethodCodex}
	d.Codex = view.Codex{SignedIn: true, Email: "a@example.invalid", Plan: "business",
		Engine: view.CodexEngine{State: "running", Supported: true},
		Limits: []view.CodexLimit{{Name: "5 時間", UsedPercent: 100, ResetsAt: time.Date(2026, 10, 25, 9, 30, 0, 0, time.UTC)}}}
	html := render(t, d)
	assert.Contains(t, html, "5 時間: 100% 使用（リセット 2026-10-25 18:30（日本時間））")
	assert.Contains(t, html, `data-testid="chatgpt-logout-codex"`)
}
