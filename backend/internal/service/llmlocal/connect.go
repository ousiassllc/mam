package llmlocal

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/llm/localllm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/safefetch"
)

// BaseURLProblem is why an address is refused, in words for the user.
func BaseURLProblem(err error) string {
	switch {
	case errors.Is(err, localcfg.ErrBaseURLScheme):
		return "ベース URL は http:// か https:// で始めてください。"
	case errors.Is(err, localcfg.ErrBaseURLParts):
		return "ベース URL に、ユーザー名・クエリ（? 以降）・フラグメント（# 以降）は付けられません。"
	case errors.Is(err, localcfg.ErrBaseURLLinkLocal):
		return "ベース URL に、リンクローカル（169.254.x.x・fe80::）、未指定、マルチキャストのアドレスは使えません。クラウドのメタデータサービスなどに届いてしまうためです。"
	}
	return "ベース URL は http://localhost:11434 のような形で入力してください。"
}

// TestInput is a server to test, saved or not. An empty BaseURL tests the saved address; an empty APIKey uses the stored key.
type TestInput struct {
	BaseURL string
	APIKey  string
}

// TestResult is the outcome of a connection test. OK is false when the server did not answer as an OpenAI-compatible server should; Message says
// what happened, and Error is the common error (#1010) of the failure: the cause and the remedy a screen or an agent shows. Models are the ids the
// server lists (at most 20 of Count).
type TestResult struct {
	OK      bool
	Message string
	Error   *apperr.Error
	Models  []string
	Count   int
}

// Test asks the server for its models (GET /v1/models), the call the model picker makes. A transport failure or an unexpected answer is a
// TestResult with OK false, not an error; an unusable address is an *InvalidError.
func (s *Service) Test(ctx context.Context, in TestInput) (TestResult, error) {
	if s.InUse() {
		return s.testManaged(ctx), nil // the address and key of the form are not what the calls use now
	}
	baseURL := strings.TrimSpace(in.BaseURL)
	if baseURL == "" {
		baseURL = s.BaseURL()
	}
	baseURL, err := localllm.NormalizeBaseURL(baseURL)
	if err != nil {
		return TestResult{}, &InvalidError{Message: BaseURLProblem(err), Field: "base_url"}
	}
	key := strings.TrimSpace(in.APIKey)
	if key == "" && s.keys != nil {
		if stored, err := s.keys.Get(provider); err == nil {
			key = stored
		}
	}
	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()
	ids, err := localllm.ListModels(ctx, s.client, baseURL, key)
	if err != nil {
		return failure(err), nil
	}
	res := TestResult{OK: true, Count: len(ids), Models: ids[:min(len(ids), maxListedModels)]}
	if len(ids) == 0 {
		res.Message = "ローカルサーバーに接続できました。ただし、使えるモデルがありません（Ollama なら `ollama pull <モデル名>`、LM Studio ならモデルをダウンロードして読み込んでください）"
	} else {
		res.Message = fmt.Sprintf("ローカルサーバーに接続できました（モデル %d 件）", len(ids))
	}
	return res, nil
}

// testManaged is the test while the calls go to the llama.cpp server of mam: it says where the server stands, and never starts it (loading a model
// takes much longer than a test; the first call starts it).
func (s *Service) testManaged(ctx context.Context) TestResult {
	info, _ := s.ManagedInfo(ctx)
	name := info.ModelName
	if name == "" {
		name = info.Model
	}
	switch info.State {
	case ManagedRunning:
		return TestResult{OK: true, Message: "この PC の llama.cpp が動いています（モデル: " + name + "）", Models: []string{info.Model}, Count: 1}
	case ManagedStopped, ManagedStarting:
		return TestResult{OK: true, Message: "この PC の llama.cpp を使います（モデル: " + name + "）。いまは止まっていて、最初の LLM の呼び出しで自動で起動します（モデルの読み込みに時間がかかります）", Models: []string{info.Model}, Count: 1}
	}
	e := apperr.New(apperr.CodeLocalLLMDown).WithDetail(info.Reason)
	return TestResult{Message: "この PC の llama.cpp を使えません: " + info.Reason, Error: e}
}

// failure is a failed test as a result that says why in words the user can act on.
func failure(err error) TestResult {
	var api *llm.APIError
	switch {
	case localllm.IsUnreachable(err), errors.Is(err, context.DeadlineExceeded):
		e := apperr.New(apperr.CodeLocalLLMDown).Wrap(err)
		return TestResult{Message: e.View(apperr.LangJa).Cause, Error: e}
	case errors.As(err, &api) && (api.Status == http.StatusUnauthorized || api.Status == http.StatusForbidden):
		e := apperr.New(apperr.CodeAuthFailed).With("http", "{{http.status}}").With("status", strconv.Itoa(api.Status)).WithDetail(api.Message)
		return TestResult{Message: "認証に失敗しました（HTTP " + strconv.Itoa(api.Status) + "）。サーバーが API キーを求めています。キーを確かめてください", Error: e}
	case errors.As(err, &api) && api.Status >= 300:
		e := apperr.New(apperr.CodeUpstream).WithCause(fmt.Sprintf("サーバーが HTTP %d を返しました。OpenAI 互換のサーバー（/v1/models がある）か、ベース URL が合っているかを確かめてください。", api.Status)).WithDetail(api.Message)
		return TestResult{Message: e.Cause, Error: e}
	case errors.Is(err, safefetch.ErrRefused):
		e := apperr.New(apperr.CodeInvalidInput).WithCause("このアドレスには接続できません（リンクローカル・未指定・マルチキャストのアドレスは使えません）。")
		return TestResult{Message: e.Cause, Error: e}
	}
	e := apperr.New(apperr.CodeUpstream).WithCause("サーバーの答えを読めませんでした。OpenAI 互換のサーバー（/v1/models がある）かを確かめてください。").WithDetail(err.Error())
	return TestResult{Message: e.Cause, Error: e}
}
