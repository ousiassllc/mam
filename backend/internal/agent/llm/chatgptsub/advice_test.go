package chatgptsub_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/llm/chatgptsub"
	"github.com/ousiassllc/mam/backend/internal/chatgpt"
)

// planTokens is a TokenSource that also tells the plan of the account (as *chatgpt.Account does).
type planTokens struct {
	tokens
	plan string
}

func (p *planTokens) Status() (chatgpt.Status, error) {
	return chatgpt.Status{SignedIn: true, Plan: p.plan}, nil
}

func notEligible(t *testing.T, src chatgptsub.TokenSource) error {
	t.Helper()
	srv := newResponses(t, func(w http.ResponseWriter, _ int) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"subscription_sharing_user_not_eligible"}}`))
	})
	_, err := chatgptsub.New(srv.URL, src, nil).Complete(context.Background(), "m", llm.Request{User: "u"})
	require.Error(t, err)
	return err
}

func noticeOf(t *testing.T, err error) string {
	t.Helper()
	var api *llm.APIError
	require.ErrorAs(t, err, &api)
	assert.True(t, api.Permanent)
	return api.Notice
}

// A workspace account that the official route refuses (403) is pointed to the Codex login in the notice of the chat.
func TestNotEligibleOnAWorkspacePlanPointsToTheCodexLogin(t *testing.T) {
	for _, plan := range []string{"business", "enterprise", "edu", "team"} {
		notice := noticeOf(t, notEligible(t, &planTokens{plan: plan}))
		assert.Contains(t, notice, "認められていません", plan)
		assert.Contains(t, notice, "Codex 経由のログイン", plan)
	}
}

func TestNotEligibleOnAPersonalPlanOrWithoutAPlanSourceKeepsTheOriginalNotice(t *testing.T) {
	assert.NotContains(t, noticeOf(t, notEligible(t, &planTokens{plan: "plus"})), "Codex")
	assert.NotContains(t, noticeOf(t, notEligible(t, &tokens{})), "Codex")
}
