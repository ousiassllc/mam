package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func (s *mcpScreen) modal(target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("HX-Request", "true")
	return s.send(req)
}

func TestMCPSettings_ThePageHasACardForTheAddressAndATableOfTokensWithButtonsThatOpenModals(t *testing.T) {
	s := newMCPScreen(t, true)

	assert.Contains(t, s.page, `id="mcp-bind-row"`)
	assert.Contains(t, s.page, `hx-get="`+paths.MCPBindEdit()+`"`)
	assert.Contains(t, s.page, `hx-get="`+paths.MCPTokenNew()+`"`)
	assert.Contains(t, s.page, `id="mcp-token-list"`, "the tbody that a new row is added to")
	assert.Contains(t, s.page, `id="mcp-tokens-empty"`)
	assert.NotContains(t, s.page, `name="bind_address"`, "the fields are in the modals, not on the page")
	assert.NotContains(t, s.page, `name="name"`)
}

func TestMCPSettings_TheModalsAreFragmentsAndAPageWithoutHTMX(t *testing.T) {
	s := newMCPScreen(t, true)

	bind := s.modal(paths.MCPBindEdit())
	require.Equal(t, http.StatusOK, bind.Code)
	assert.Contains(t, bind.Body.String(), `<h2 id="mam-modal-title"`)
	assert.Contains(t, bind.Body.String(), `value="127.0.0.1"`, "the saved address")
	assert.Contains(t, bind.Body.String(), `<option value="100.101.102.103">`, "the addresses of this machine are suggested")
	assert.Contains(t, bind.Body.String(), `hx-put="`+paths.MCPBind()+`"`)
	assert.Contains(t, bind.Body.String(), `method="post"`, "a form sent by the browser before htmx has it is no GET")

	issue := s.modal(paths.MCPTokenNew())
	require.Equal(t, http.StatusOK, issue.Code)
	assert.Contains(t, issue.Body.String(), `hx-post="`+paths.MCPTokens()+`"`)
	assert.Contains(t, issue.Body.String(), `method="post"`)
	assert.NotContains(t, issue.Body.String(), "mam_mcp_")

	for _, target := range []string{paths.MCPBindEdit(), paths.MCPTokenNew()} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		rec := s.send(req)
		require.Equal(t, http.StatusOK, rec.Code, target)
		assert.Contains(t, rec.Body.String(), `<nav aria-label="設定"`, "an address typed in is a page, not a bare fragment")
		assert.Contains(t, rec.Body.String(), `id="mcp-bind-row"`)
		assert.NotContains(t, rec.Body.String(), `id="mam-modal-title"`)
	}
}

func TestMCPSettings_ASavedBindAnswersTheCardAndClosesTheModalAndARefusedOneKeepsTheModal(t *testing.T) {
	s := newMCPScreen(t, true)

	ok := s.act(http.MethodPut, paths.MCPBind(), url.Values{"bind_address": {"100.101.102.103"}}, true)
	require.Equal(t, http.StatusOK, ok.Code)
	assert.Equal(t, "modal-close", ok.Header().Get("HX-Trigger-After-Swap"))
	assert.Contains(t, ok.Body.String(), `id="mcp-bind-row"`)
	assert.Contains(t, ok.Body.String(), `hx-swap-oob="true"`)
	assert.NotContains(t, ok.Body.String(), `id="mcp-panel"`, "the whole category is not drawn again")

	refused := s.act(http.MethodPut, paths.MCPBind(), url.Values{"bind_address": {"0.0.0.0"}}, true)
	assert.Equal(t, http.StatusUnprocessableEntity, refused.Code)
	assert.Empty(t, refused.Header().Get("HX-Trigger-After-Swap"), "a refused address leaves the modal open")
	assert.Contains(t, refused.Body.String(), `id="mam-modal-error"`)
	assert.Contains(t, refused.Body.String(), `aria-invalid="true"`)
}

func TestMCPSettings_ARefusedNameKeepsTheIssueModalAndNeverShowsAToken(t *testing.T) {
	s := newMCPScreen(t, true)

	rec := s.act(http.MethodPost, paths.MCPTokens(), url.Values{"name": {"  "}}, true)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), `id="mam-modal-error"`)
	assert.Contains(t, rec.Body.String(), `hx-post="`+paths.MCPTokens()+`"`, "the form to try again")
	assert.NotContains(t, rec.Body.String(), "mam_mcp_")
	list, err := s.tokens.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestMCPSettings_TheValueIsInOneAnswerOnlyAndNotInTheRowOfTheTable(t *testing.T) {
	s := newMCPScreen(t, true)

	rec := s.act(http.MethodPost, paths.MCPTokens(), url.Values{"name": {"Grok Bot"}}, true)
	require.Equal(t, http.StatusCreated, rec.Code)
	value := issuedValue(t, rec.Body.String())
	row := rec.Body.String()
	row = row[strings.Index(row, `<tr id="mcp-token-`):]
	row = row[:strings.Index(row, "</tr>")]
	assert.NotContains(t, row, value, "the row that joins the table holds the start of the token only")
	assert.Contains(t, row, "mam_mcp_")
	assert.Contains(t, row, "未使用")

	list, err := s.tokens.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 1)
	revoked := s.act(http.MethodDelete, paths.MCPToken(list[0].ID), nil, true)
	require.Equal(t, http.StatusOK, revoked.Code)
	assert.True(t, strings.HasPrefix(revoked.Body.String(), `<tr id="mcp-token-`), "the answer is the row of the token, first: %s", revoked.Body.String())
	assert.NotContains(t, revoked.Body.String(), value)
	assert.Contains(t, revoked.Body.String(), "トークンを失効しました")
}
