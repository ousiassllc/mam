package mcpkit_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/credential"
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
		"key store":       {fmt.Errorf("save: %w", &credential.StoreError{Message: "読み戻せませんでした", Err: errors.New("vault locked")}), "unavailable", 503, "読み戻せませんでした"},
		"locked":          {&service.LockedError{Parameters: []domain.LockParameter{domain.ParamPrompt}}, "locked", 423, "LOCK"},
		"budget":          {service.ErrBudgetExceeded, "budget_exceeded", 402, "予算"},
		"unavailable":     {service.ErrUnavailable, "unavailable", 503, "生成機能"},
		"brand":           {service.ErrBrandViolation, "forbidden", 403, "Brand"},
		"rate limit":      {rateLimited, "rate_limited", 429, "上限"},
		"ai failure":      {&service.AIError{Reason: "回答を読み取れませんでした"}, "upstream", 502, "読み取れません"},
		"unknown":         {errors.New(`open /home/me/.mam/assets/x.mp4: permission denied`), "internal", 500, "原因がわかりませんでした"},
	} {
		te := mcpkit.MapError(tc.err)
		assert.Equal(t, tc.code, te.Code, name)
		assert.Equal(t, tc.status, te.Status, name)
		assert.Contains(t, te.Message, tc.text, name)
		first, _, _ := strings.Cut(te.Error(), "\n") // a failure the app knows has its structure as the second line (#1010)
		assert.Equal(t, tc.code+": "+te.Message, first, name)
	}
	te := mcpkit.MapError(errors.New(`open /home/me/.mam/assets/x.mp4: permission denied`))
	assert.NotContains(t, te.Message, "/home/me", "an unknown error shows none of its text")
	require.NotNil(t, te.Failure, "an unknown error is told as the common error too")
	assert.Equal(t, apperr.CodeUnknown, te.Failure.Code)
	assert.Empty(t, te.Failure.Detail, "its text is not told: it can carry a path (the log has it)")
	assert.Contains(t, te.Error(), `"code":"unknown"`, "the second line has the structure")
	assert.True(t, mcpkit.Internal(errors.New("x")))
	assert.False(t, mcpkit.Internal(service.ErrNotFound))
}

// A failure told as the common error (#1010) is the tool error of its class, with the cause and the remedy in the message and the
// structure (code, cause, remedy, settings, detail) in the failure and as the second line of the text.
func TestMapError_TheCommonErrorKeepsItsCodeCauseRemedyAndDetail(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		code   apperr.Code
		class  string
		status int
		paths  []string
	}{
		"needs first_frame": {&modeladapter.ProviderError{Vendor: "endpoint:bundled-comfyui", Status: 422, Message: "the workflow needs first_frame (12), the request has none"},
			apperr.CodeInputMissing, "validation", 422, nil},
		"LLM key":      {fmt.Errorf("x: %w", llm.ErrNoAPIKey), apperr.CodeLLMKeyMissing, "unavailable", 503, []string{"/settings/api-keys", "/settings/llm"}},
		"not reached":  {&modeladapter.ProviderError{Vendor: "endpoint:bundled-comfyui", Err: &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}}, apperr.CodeConnectionFailed, "upstream", 502, []string{"/model-manager", "/settings/local-models"}},
		"VRAM":         {&modeladapter.ProviderError{Vendor: "endpoint:bundled-comfyui", Status: 422, Message: "KSampler: CUDA out of memory"}, apperr.CodeVRAM, "upstream", 502, nil},
		"model":        {&modeladapter.ProviderError{Vendor: "endpoint:bundled-comfyui", Status: 400, Message: "Value not in list: unet_name: 'x.safetensors' not in []"}, apperr.CodeModelMissing, "unavailable", 503, []string{"/model-manager"}},
		"429 of a key": {&modeladapter.ProviderError{Vendor: "flux", Status: 429, Message: "slow down"}, apperr.CodeRateLimited, "rate_limited", 429, nil},
		"own error":    {fmt.Errorf("wrap: %w", apperr.New(apperr.CodeLicense).WithDetail("consent missing")), apperr.CodeLicense, "conflict", 409, []string{"/model-manager"}},
	} {
		t.Run(name, func(t *testing.T) {
			te := mcpkit.MapError(tc.err)
			require.NotNil(t, te.Failure)
			assert.Equal(t, tc.code, te.Failure.Code)
			assert.Equal(t, tc.class, te.Code, "the code is the class a tool error always had")
			assert.Equal(t, tc.status, te.Status)
			assert.NotEmpty(t, te.Failure.Cause)
			assert.NotEmpty(t, te.Failure.Remedy)
			assert.Equal(t, te.Failure.Cause+te.Failure.Remedy, te.Message, "the message is the cause, then the remedy")
			var paths []string
			for _, l := range te.Failure.Settings {
				paths = append(paths, l.Path)
			}
			assert.Equal(t, tc.paths, paths)

			first, second, found := strings.Cut(te.Error(), "\n")
			require.True(t, found, "the second line is the structure")
			assert.Equal(t, tc.class+": "+te.Message, first, "the first line is what it always was")
			var back apperr.View
			require.NoError(t, json.Unmarshal([]byte(second), &back))
			assert.Equal(t, *te.Failure, back)
		})
	}
	te := mcpkit.MapError(&modeladapter.ProviderError{Vendor: "endpoint:x", Status: 422, Message: "the workflow needs first_frame (12), the request has none"})
	assert.Contains(t, te.Failure.Cause, "first_frame")
	assert.Contains(t, te.Failure.Detail, "needs first_frame (12)")
	assert.Equal(t, "input_missing", string(te.Failure.Code))
}

// An internal failure keeps its id in the message and, when it is a failure of the common error, in the failure.
func TestReportError_AnUnknownCommonErrorCarriesItsIDInTheFailure(t *testing.T) {
	te := mcpkit.ReportError(nil, "x", apperr.New(apperr.CodeUnknown).WithDetail("odd"))
	assert.Equal(t, mcpkit.CodeInternal, te.Code)
	require.NotNil(t, te.Failure)
	assert.NotEmpty(t, te.ErrorID)
	assert.Equal(t, te.ErrorID, te.Failure.ErrorID)
	assert.Contains(t, te.Message, te.ErrorID)
}
