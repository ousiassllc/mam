package credentialpanel_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/organisms/credentialpanel"
)

func TestPanel_RendersTheProviderRowsFromTheViewType(t *testing.T) {
	var sb strings.Builder
	err := credentialpanel.Panel(credentialpanel.Data{Providers: []credentialpanel.ProviderView{
		{Provider: "kling", Label: "Kling", Configured: true},
		{Provider: "runway", Label: "Runway", Configured: false},
	}}).Render(context.Background(), &sb)
	require.NoError(t, err)
	html := sb.String()
	assert.Contains(t, html, `data-provider="kling"`)
	assert.Contains(t, html, `data-provider="runway"`)
	assert.Contains(t, html, "設定済み")
	assert.Contains(t, html, "未設定")
	assert.Contains(t, html, `hx-delete="/credentials/kling"`)
	assert.NotContains(t, html, `hx-delete="/credentials/runway"`)
}

// The delete button sits inside the <form> that holds the password input. htmx would send the form's values with a
// DELETE as URL query parameters, leaking a pasted-but-unsaved API key into the URL (#504); hx-params="none" stops it.
func TestPanel_DeleteButtonSendsNoFormValues(t *testing.T) {
	var sb strings.Builder
	err := credentialpanel.Panel(credentialpanel.Data{Providers: []credentialpanel.ProviderView{
		{Provider: "kling", Label: "Kling", Configured: true},
	}}).Render(context.Background(), &sb)
	require.NoError(t, err)

	button := regexp.MustCompile(`<button[^>]*\shx-delete=[^>]*>`).FindString(sb.String())
	require.NotEmpty(t, button, "the configured provider renders a delete button")
	assert.Contains(t, button, `hx-params="none"`)
}

// The key typed into one provider's row survives the save or delete of another provider: a row answers for itself.
func TestRow_AnswersForItsProviderOnly(t *testing.T) {
	var sb strings.Builder
	require.NoError(t, credentialpanel.Row(credentialpanel.ProviderView{Provider: "kling", Label: "Kling", Configured: true}, "鍵が正しくありません").Render(context.Background(), &sb))
	html := sb.String()

	assert.Contains(t, html, `id="credential-row-kling"`)
	assert.Contains(t, html, `hx-target="#credential-row-kling"`)
	assert.Contains(t, html, `hx-delete="/credentials/kling"`)
	assert.Contains(t, html, "鍵が正しくありません")
	assert.NotContains(t, html, `id="credential-panel"`)
	assert.NotContains(t, html, "runway")
}
