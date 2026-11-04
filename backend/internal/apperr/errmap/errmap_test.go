package errmap_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/direct"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/topaz"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/apperr/errmap"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

func provider(status int, msg string) error {
	return &modeladapter.ProviderError{Vendor: "endpoint:bundled-comfyui", Status: status, Message: msg}
}

// The table of the issue: every kind of failure an outside service sends has its code, and the original message stays as the detail.
func TestExplain_ClassifiesTheFailuresOfOutsideServices(t *testing.T) {
	rejected := fmt.Errorf("generate: %w", provider(422, "the workflow needs first_frame (12), the request has none"))
	for name, tc := range map[string]struct {
		err  error
		want apperr.Code
	}{
		"422 needs first_frame":    {rejected, apperr.CodeInputMissing},
		"401":                      {provider(401, "bad key"), apperr.CodeAuthFailed},
		"403":                      {provider(403, "forbidden"), apperr.CodeAuthFailed},
		"404":                      {provider(404, "no such model"), apperr.CodeRemoteNotFound},
		"408":                      {provider(408, "slow"), apperr.CodeTimeout},
		"422 other":                {provider(422, "invalid prompt"), apperr.CodeRequestRejected},
		"400":                      {provider(400, "bad"), apperr.CodeRequestRejected},
		"402":                      {provider(402, "no credits"), apperr.CodeCreditsInsufficient},
		"Topaz key":                {topaz.ErrNoAPIKey, apperr.CodeKeyMissing},
		"Topaz maintenance":        {&topaz.Error{Code: topaz.CodeServerMaintenance, Status: 503, Err: provider(503, "maintenance")}, apperr.CodeTopazMaintenance},
		"Topaz 503 with a code":    {&topaz.Error{Code: "SERVER_ERROR", Status: 503, Err: provider(503, "x")}, apperr.CodeServerError},
		"Topaz credit difference":  {&topaz.Error{Code: topaz.CodeCreditDifference}, apperr.CodeTopazCreditDiff},
		"Topaz timeout":            {&topaz.Error{Code: topaz.CodeTimeoutQueue}, apperr.CodeTopazJobTimeout},
		"Topaz failed job":         {&topaz.Error{Code: "PROCESSING_FAILURE"}, apperr.CodeTopazJobFailed},
		"Topaz cancelled":          {&topaz.Error{Code: topaz.CodeCanceled}, apperr.CodeTopazJobFailed},
		"Topaz invalid source":     {&topaz.Error{Code: topaz.CodeInvalidInput}, apperr.CodeTopazSourceInvalid},
		"Topaz transfer":           {&topaz.Error{Code: topaz.CodeTransferFailed, Err: errors.New("x")}, apperr.CodeTopazTransferFailed},
		"429":                      {provider(429, "slow down"), apperr.CodeRateLimited},
		"500":                      {provider(500, "oops"), apperr.CodeServerError},
		"503":                      {provider(503, "busy"), apperr.CodeServerError},
		"vendor timeout":           {fmt.Errorf("x: %w", modeladapter.ErrTimeout), apperr.CodeTimeout},
		"context deadline":         {fmt.Errorf("x: %w", context.DeadlineExceeded), apperr.CodeTimeout},
		"no connection":            {&modeladapter.ProviderError{Vendor: "endpoint:comfy", Err: &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}}, apperr.CodeConnectionFailed},
		"no answer":                {&modeladapter.ProviderError{Vendor: "endpoint:comfy", Err: errors.New("EOF")}, apperr.CodeConnectionFailed},
		"CUDA OOM as a 422":        {provider(422, "KSampler: CUDA out of memory. Tried to allocate 2.00 GiB"), apperr.CodeVRAM},
		"torch OOM":                {errors.New("torch.OutOfMemoryError: Allocation on device"), apperr.CodeVRAM},
		"disk (message)":           {provider(422, "SaveVideo: [Errno 28] No space left on device"), apperr.CodeDiskFull},
		"disk (errno)":             {fmt.Errorf("write: %w", syscall.ENOSPC), apperr.CodeDiskFull},
		"disk (download)":          {&fetch.NoSpaceError{Need: 10 << 30, Free: 1 << 30, Path: "/models"}, apperr.CodeDiskFull},
		"model not installed":      {provider(400, "Value not in list: unet_name: 'wan2.2.safetensors' not in ['a.safetensors']"), apperr.CodeModelMissing},
		"node missing":             {provider(400, `{"type":"missing_node_type","message":"Node 'X' not found"}`), apperr.CodeNodeMissing},
		"license":                  {errors.New("the license has to be accepted before the install"), apperr.CodeLicense},
		"model API key":            {fmt.Errorf("%w: openai", modeladapter.ErrNoAPIKey), apperr.CodeKeyMissing},
		"LLM key":                  {fmt.Errorf("%w: openai", llm.ErrNoAPIKey), apperr.CodeLLMKeyMissing},
		"LLM 429":                  {&llm.APIError{Provider: llm.ProviderOpenAI, Status: 429}, apperr.CodeRateLimited},
		"LLM invalid":              {llm.ErrInvalidOutput, apperr.CodeLLMInvalidOutput},
		"circuit":                  {modeladapter.ErrCircuitOpen, apperr.CodeCircuitOpen},
		"unsupported input":        {fmt.Errorf("%w: first frame", direct.ErrInputsUnsupported), apperr.CodeInputUnsupported},
		"stalled":                  {localcfg.MarkStall(30*time.Minute, errors.New("t")), apperr.CodeStalled},
		"endpoint timeout":         {&localcfg.TimeoutError{After: 15 * time.Minute, Err: errors.New("t")}, apperr.CodeEndpointTimeout},
		"download 401":             {&fetch.HTTPError{Status: 401, URL: "https://x"}, apperr.CodeAuthFailed},
		"unknown":                  {errors.New("something odd"), apperr.CodeUnknown},
		"vendor answered with 200": {&modeladapter.ProviderError{Vendor: "v", Status: 200, Message: "the answer has no image"}, apperr.CodeUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			e := errmap.Explain(tc.err)
			require.NotNil(t, e)
			assert.Equal(t, tc.want, e.Code)
			assert.Contains(t, e.Detail, tc.err.Error()[:min(10, len(tc.err.Error()))], "the original message stays as the detail")
			assert.ErrorIs(t, e, tc.err, "the failure behind stays reachable")
			v := e.View(apperr.LangJa)
			assert.NotEmpty(t, v.Cause)
			assert.NotEmpty(t, v.Remedy)
			assert.NotContains(t, v.Cause+v.Remedy, "{")
		})
	}
	assert.Nil(t, errmap.Explain(nil))
}

func TestExplain_NeedsFirstFrameNamesTheInputAndTheNode(t *testing.T) {
	e := errmap.Explain(provider(http.StatusUnprocessableEntity, "the workflow needs first_frame (12), the request has none"))
	assert.Equal(t, "first_frame", e.Args["param"])
	assert.Equal(t, "12", e.Args["node"])
	v := e.View(apperr.LangJa)
	assert.Contains(t, v.Cause, "first_frame")
	assert.Contains(t, v.Remedy, "最初のフレーム")
	assert.Contains(t, v.Detail, "needs first_frame (12)")
}

func TestExplain_AnAppErrorInTheChainIsReturnedAsItIs(t *testing.T) {
	own := apperr.New(apperr.CodeConflict).WithCause("すでに生成中です。")
	assert.Same(t, own, errmap.Explain(fmt.Errorf("wrap: %w", own)))
}

func TestExplain_AProviderNoticeKeepsTheProvidersWords(t *testing.T) {
	e := errmap.Explain(&llm.APIError{Provider: llm.ProviderChatGPTSubscription, Notice: "ChatGPT の利用上限に達しました", Permanent: true})
	assert.Equal(t, apperr.CodeUpstream, e.Code)
	assert.Equal(t, "ChatGPT の利用上限に達しました", e.View(apperr.LangJa).Cause)
}

func TestExplain_ADiskFullNamesThePlace(t *testing.T) {
	e := errmap.Explain(&fetch.NoSpaceError{Need: 10 << 30, Free: 1 << 30, Path: "/models"})
	assert.Contains(t, e.View(apperr.LangJa).Cause, "（場所 /models）")
	assert.Contains(t, e.View(apperr.LangEn).Cause, "(at /models)")
}

func TestExplain_LongDetailIsCut(t *testing.T) {
	e := errmap.Explain(errors.New(string(make([]rune, 5000))))
	assert.LessOrEqual(t, len([]rune(e.Detail)), 1001)
}

// An unknown failure is worded as unknown and logged under the correlation id that the error carries.
func TestReport_LogsAnUnknownFailureWithItsID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	e := errmap.Report(logger, "generation failed", errors.New("something odd"), "result_id", "r1")
	assert.Equal(t, apperr.CodeUnknown, e.Code)
	require.NotEmpty(t, e.ID)
	assert.Contains(t, buf.String(), "error_id="+e.ID)
	assert.Contains(t, buf.String(), "something odd")
	assert.Contains(t, buf.String(), "result_id=r1")
	assert.Equal(t, e.ID, e.View(apperr.LangJa).ErrorID)

	buf.Reset()
	known := errmap.Report(logger, "generation failed", provider(429, "slow"))
	assert.Equal(t, apperr.CodeRateLimited, known.Code)
	assert.Empty(t, known.ID)
	assert.Empty(t, buf.String(), "a known failure is not logged by Report")
}

func TestLocalTimeoutSaysWhichLimitWasHit(t *testing.T) {
	cause := errors.New("timeout")
	stalled := errmap.LocalTimeout(localcfg.MarkStall(30*time.Minute, cause))
	require.NotNil(t, stalled)
	assert.Contains(t, stalled.View(apperr.LangJa).Cause, "30分のあいだ進まなかった")
	assert.Contains(t, stalled.View(apperr.LangJa).Remedy, "タイムアウト（進みが止まってから打ち切るまでの時間）を延ばして")

	ceiling := errmap.LocalTimeout(&localcfg.TimeoutError{After: localcfg.MaxTimeoutSeconds * time.Second, Err: cause})
	assert.Contains(t, ceiling.View(apperr.LangJa).Cause, "全体の上限（24時間）")
	assert.NotContains(t, ceiling.View(apperr.LangJa).Remedy, "延ばす")

	whole := errmap.LocalTimeout(&localcfg.TimeoutError{After: 15 * time.Minute, Err: cause})
	assert.Contains(t, whole.View(apperr.LangJa).Cause, "接続先の待ち時間（15分）")
	assert.Contains(t, whole.View(apperr.LangJa).Remedy, "タイムアウトを延ばす")

	assert.Nil(t, errmap.LocalTimeout(cause))
}
