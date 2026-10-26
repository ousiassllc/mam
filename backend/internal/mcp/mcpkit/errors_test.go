package mcpkit_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

func TestMapError_GivesTheScreensStatusAndAMessageThatKeepsNoInternals(t *testing.T) {
	rateLimited := &service.AIError{Reason: "LLM の呼び出し回数が上限です", Err: &llm.APIError{Status: http.StatusTooManyRequests}}
	for name, tc := range map[string]struct {
		err    error
		code   string
		status int
		text   string // a part of the message
	}{
		"validation":      {&service.ValidationError{Field: "name", Message: "名前を入力してください"}, "validation", 422, "名前を入力してください"},
		"wrapped service": {fmt.Errorf("load: %w", service.ErrNotFound), "not_found", 404, "見つかりません"},
		"conflict":        {&service.ConflictError{Message: "別の編集処理が走っています"}, "conflict", 409, "別の編集処理が走っています"},
		"busy":            {service.ErrBusy, "conflict", 409, "生成中"},
		"locked":          {&service.LockedError{Parameters: []domain.LockParameter{domain.ParamPrompt}}, "locked", 423, "LOCK"},
		"budget":          {service.ErrBudgetExceeded, "budget_exceeded", 402, "予算"},
		"unavailable":     {service.ErrUnavailable, "unavailable", 503, "生成機能"},
		"brand":           {service.ErrBrandViolation, "forbidden", 403, "Brand"},
		"rate limit":      {rateLimited, "rate_limited", 429, "上限"},
		"ai failure":      {&service.AIError{Reason: "回答を読み取れませんでした"}, "upstream", 502, "読み取れません"},
		"unknown":         {errors.New(`open /home/me/.mam/assets/x.mp4: permission denied`), "internal", 500, "ログを確認"},
	} {
		te := mcpkit.MapError(tc.err)
		assert.Equal(t, tc.code, te.Code, name)
		assert.Equal(t, tc.status, te.Status, name)
		assert.Contains(t, te.Message, tc.text, name)
		assert.Equal(t, tc.code+": "+te.Message, te.Error(), name)
	}
	te := mcpkit.MapError(errors.New(`open /home/me/.mam/assets/x.mp4: permission denied`))
	assert.NotContains(t, te.Message, "/home/me", "an unknown error shows none of its text")
	assert.True(t, mcpkit.Internal(errors.New("x")))
	assert.False(t, mcpkit.Internal(service.ErrNotFound))
}
