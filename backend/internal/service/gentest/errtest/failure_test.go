package errtest_test

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
)

// failedResult runs one image of model with the fake failing with err and returns the result that failed.
func failedResult(t *testing.T, opts []gentesttest.Option, model domain.ModelName, err error) (*gentesttest.Rig, record.ResultView) {
	t.Helper()
	r := gentesttest.New(t, opts...)
	r.Fake.FailWith(model, err)
	ref, runErr := r.Confirmed(gentest.Request{Media: "image", Models: []string{string(model)}, Prompts: []gentest.PromptInput{{Prompt: "a fox"}}})
	require.NoError(t, runErr)
	st := r.Wait(ref.BatchID)
	require.Len(t, st.Results, 1)
	require.Equal(t, gentestrepo.StatusFailed, st.Results[0].Status)
	return r, st.Results[0]
}

func comfy(status int, msg string) error {
	return &modeladapter.ProviderError{Vendor: "endpoint:bundled-comfyui", Status: status, Message: msg}
}

// The failures of the issue (#1010) reach the history as the common error: the code, the cause and the remedy in Japanese, the
// screens that help, and the vendor's own words as the detail (and nothing of "ベンダーがリクエストを拒否しました: modeladapter: ...").
func TestFailedResultCarriesTheCommonError(t *testing.T) {
	for name, tc := range map[string]struct {
		err      error
		code     apperr.Code
		cause    string
		remedy   string
		settings string
	}{
		"422 needs first_frame": {comfy(http.StatusUnprocessableEntity, "the workflow needs first_frame (12), the request has none"),
			apperr.CodeInputMissing, "first_frame", "最初のフレーム", ""},
		"no API key":     {fmt.Errorf("%w: flux", modeladapter.ErrNoAPIKey), apperr.CodeKeyMissing, "API キーが設定されていません", "API キーとログイン", "/settings/api-keys"},
		"not connected":  {&modeladapter.ProviderError{Vendor: "endpoint:bundled-comfyui", Err: &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}}, apperr.CodeConnectionFailed, "接続先につながりませんでした", "起動しているか", "/model-manager"},
		"VRAM":           {comfy(http.StatusUnprocessableEntity, "KSampler: CUDA out of memory"), apperr.CodeVRAM, "VRAM", "解像度や秒数を小さく", ""},
		"model missing":  {comfy(http.StatusBadRequest, "Value not in list: unet_name: 'wan.safetensors' not in []"), apperr.CodeModelMissing, "モデルのファイル", "導入", "/model-manager"},
		"disk":           {comfy(http.StatusUnprocessableEntity, "SaveImage: [Errno 28] No space left on device"), apperr.CodeDiskFull, "ディスクの空き容量", "空きを作って", ""},
		"401":            {comfy(http.StatusUnauthorized, "bad key"), apperr.CodeAuthFailed, "認証を拒否", "キーを確かめて", "/settings/api-keys"},
		"429":            {comfy(http.StatusTooManyRequests, "slow down"), apperr.CodeRateLimited, "429", "しばらく待って", ""},
		"500":            {comfy(http.StatusBadGateway, "oops"), apperr.CodeServerError, "障害", "しばらく待って", ""},
		"422 other text": {comfy(http.StatusUnprocessableEntity, "prompt too long"), apperr.CodeRequestRejected, "拒否しました", "見直して", ""},
	} {
		t.Run(name, func(t *testing.T) {
			_, res := failedResult(t, nil, domain.ModelFlux2, tc.err)

			require.NotNil(t, res.Failure)
			assert.Equal(t, tc.code, res.Failure.Code)
			assert.Contains(t, res.Failure.Cause, tc.cause)
			assert.Contains(t, res.Failure.Remedy, tc.remedy)
			assert.NotEmpty(t, res.Failure.Class)
			assert.Contains(t, res.Failure.Detail, tc.err.Error()[:20], "the vendor's own words are the detail")
			var paths []string
			for _, l := range res.Failure.Settings {
				paths = append(paths, l.Path)
			}
			if tc.settings != "" {
				assert.Contains(t, paths, tc.settings)
			}
			assert.NotContains(t, res.Error, "ベンダーがリクエストを拒否しました")
			assert.Contains(t, res.Error, res.Failure.Cause, "the one-line text starts with the cause")
		})
	}
}

// The "needs first_frame" 422 names the input and the node, in words a person can act on (#1009 builds on it).
func TestFailedResultNeedsFirstFrameNamesTheInput(t *testing.T) {
	_, res := failedResult(t, nil, domain.ModelFlux2, comfy(http.StatusUnprocessableEntity, "the workflow needs first_frame (12), the request has none"))

	assert.Equal(t, "このモデルが必ず使う入力（first_frame）が渡されていません。", res.Failure.Cause)
	assert.Contains(t, res.Failure.Detail, "needs first_frame (12)")
}

// A failure nobody knows is told as unknown with its detail, and logged under the id the failure carries.
func TestUnknownFailureIsLoggedWithItsID(t *testing.T) {
	var (
		mu  sync.Mutex
		buf bytes.Buffer
	)
	logger := slog.New(slog.NewTextHandler(syncWriter{&mu, &buf}, nil))
	_, res := failedResult(t, []gentesttest.Option{func(_ *gentesttest.Rig, d *gentest.Deps) { d.Logger = logger }}, domain.ModelFlux2, errors.New("something odd happened"))

	require.NotNil(t, res.Failure)
	assert.Equal(t, apperr.CodeUnknown, res.Failure.Code)
	assert.Contains(t, res.Failure.Cause, "原因がわかりませんでした")
	assert.Equal(t, "something odd happened", res.Failure.Detail)
	require.NotEmpty(t, res.Failure.ErrorID)
	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, buf.String(), "error_id="+res.Failure.ErrorID)
	assert.Contains(t, buf.String(), "something odd happened")
}

// A refusal of the app itself (the Explain of the model manager) is the common error too.
func TestExplainedRefusalKeepsItsCode(t *testing.T) {
	explain := func(err error) *apperr.Error {
		if strings.Contains(err.Error(), "license") {
			return apperr.New(apperr.CodeLicense).Wrap(err)
		}
		return nil
	}
	_, res := failedResult(t, []gentesttest.Option{func(_ *gentesttest.Rig, d *gentest.Deps) { d.Explain = explain }}, domain.ModelFlux2, errors.New("license not accepted"))

	require.NotNil(t, res.Failure)
	assert.Equal(t, apperr.CodeLicense, res.Failure.Code)
	assert.Contains(t, res.Failure.Remedy, "同意してから導入")
}

type syncWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (s syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}
