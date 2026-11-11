package settings

import (
	"context"
	"errors"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/apperr/errmap"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
)

// LocalServer is the local OpenAI-compatible LLM server (provider "local", #1008) as saved: its address, whether an API key is stored (never the key) and
// the unload switch.
type LocalServer struct {
	Available              bool   `json:"available" jsonschema:"false ならこのアプリにはローカル LLM サーバーの設定がない"`
	BaseURL                string `json:"base_url,omitempty" jsonschema:"LLM の呼び出しとモデル一覧が向かうアドレス（末尾の /v1 は含まない）"`
	DefaultBaseURL         string `json:"default_base_url,omitempty"`
	KeyConfigured          bool   `json:"key_configured" jsonschema:"API キーを保存しているか（任意のキー。値は返さない）"`
	UnloadBeforeGeneration bool   `json:"unload_before_generation" jsonschema:"画像・動画を作る前に Ollama のモデルをメモリから降ろすか（keep_alive=0）"`
	UseManagedRuntime      bool   `json:"use_managed_runtime" jsonschema:"true ならアドレスを使わず、mam が導入・起動する llama.cpp（モデルマネージャーのローカル LLM の llama-cpp）に向かう"`
	UseManagedOllama       bool   `json:"use_managed_ollama" jsonschema:"true ならアドレスを使わず、mam が導入・起動する Ollama（モデルマネージャーのローカル LLM）に向かう。use_managed_runtime と同時には true にならない"`
	Use                    string `json:"use,omitempty" jsonschema:"呼び出しの行き先: llama-cpp（mam の llama.cpp）・ollama（mam の Ollama）・custom（base_url のサーバー）"`
	Pinned                 bool   `json:"pinned" jsonschema:"起動オプションで設定ファイルが固定されていて、アドレスとスイッチは変えられない"`
	Problem                string `json:"problem,omitempty" jsonschema:"設定ファイルを読めない理由"`
	// ManagedRuntime is where mam's own llama.cpp stands (absent when the app has none).
	ManagedRuntime *ManagedRuntime `json:"managed_runtime,omitempty"`
}

// ManagedRuntime is the llama.cpp server that mam installs and starts itself, as the settings see it.
type ManagedRuntime struct {
	State  string `json:"state" jsonschema:"unsupported（この PC では使えない）・not_installed（未導入か重みが不足）・stopped・starting・running・failed"`
	Reason string `json:"reason,omitempty" jsonschema:"使えない・失敗の理由と、何をすればよいか"`
	Model  string `json:"model,omitempty" jsonschema:"使うモデルの id（LLM の割り当ての model に渡す名前）。選び方は select_local_runtime_model"`
}

func localServerOf(svc *llmlocal.Service) LocalServer {
	if svc == nil {
		return LocalServer{}
	}
	v := svc.View()
	out := LocalServer{
		Available: true, BaseURL: v.BaseURL, DefaultBaseURL: v.DefaultBaseURL, KeyConfigured: v.KeyConfigured,
		UnloadBeforeGeneration: v.UnloadBeforeGeneration, UseManagedRuntime: v.UseManaged, UseManagedOllama: v.UseManagedOllama, Use: string(v.Use),
		Pinned: v.Pinned, Problem: v.Problem,
	}
	if info, ok := svc.ManagedInfo(context.Background()); ok {
		out.ManagedRuntime = &ManagedRuntime{State: string(info.State), Reason: info.Reason, Model: info.Model}
	}
	return out
}

// SetLocalServerInput is what to change; a field that is left out keeps what is saved.
type SetLocalServerInput struct {
	BaseURL                string `json:"base_url,omitempty" jsonschema:"ローカルサーバーのアドレス（http://localhost:11434 は Ollama、http://localhost:1234 は LM Studio）。http・LAN・tailnet も可。リンクローカルは不可。末尾の /v1 は付けても外して保存される"`
	APIKey                 string `json:"api_key,omitempty" jsonschema:"サーバーが API キーを求めるときだけ。OS のセキュアストレージ（プロバイダー local）に保存され、応答には含まれない"`
	ClearAPIKey            bool   `json:"clear_api_key,omitempty" jsonschema:"true なら保存したキーを削除する"`
	UnloadBeforeGeneration *bool  `json:"unload_before_generation,omitempty" jsonschema:"画像・動画を作る前に Ollama のモデルを降ろすか。Ollama 以外では何もしない"`
	UseManagedRuntime      *bool  `json:"use_managed_runtime,omitempty" jsonschema:"true で「この PC の llama.cpp を使う」（mam が導入・起動する llama.cpp。起動中はそのポートを自動で使い、画像・動画を作る前に止める）。false でアドレスに戻す"`
	UseManagedOllama       *bool  `json:"use_managed_ollama,omitempty" jsonschema:"true で「Ollama を使う」（mam が導入・起動する Ollama。use_managed_runtime と同時に true にはできない）。false でアドレスに戻す"`
}

func (h handlers) setLocalServer(_ context.Context, in SetLocalServerInput) (LocalServer, error) {
	svc := h.r.Settings.LLMLocal
	if svc == nil {
		return LocalServer{}, Unavailable("ローカル LLM サーバーの設定")
	}
	if in.BaseURL == "" && in.APIKey == "" && !in.ClearAPIKey && in.UnloadBeforeGeneration == nil && in.UseManagedRuntime == nil && in.UseManagedOllama == nil {
		return LocalServer{}, invalidInput("base_url", "変える項目（base_url・api_key・clear_api_key・unload_before_generation・use_managed_runtime・use_managed_ollama）を 1 つ以上指定してください")
	}
	err := svc.Save(llmlocal.SaveInput{BaseURL: in.BaseURL, APIKey: in.APIKey, ClearKey: in.ClearAPIKey, Unload: in.UnloadBeforeGeneration, UseManaged: in.UseManagedRuntime, UseManagedOllama: in.UseManagedOllama})
	var invalid *llmlocal.InvalidError
	var store *credential.StoreError
	switch {
	case err == nil:
		return localServerOf(svc), nil
	case errors.As(err, &invalid):
		return LocalServer{}, invalidInput(invalid.Field, invalid.Message)
	case errors.Is(err, llmlocal.ErrManagedUnavailable):
		return LocalServer{}, invalidInput("use_managed_runtime", "このアプリには、mam が管理する llama.cpp がありません")
	case errors.Is(err, llmlocal.ErrManagedOllamaUnavailable):
		return LocalServer{}, invalidInput("use_managed_ollama", "このアプリには、mam が管理する Ollama がありません")
	case errors.Is(err, llmlocal.ErrPinned):
		return LocalServer{}, &service.ConflictError{Message: pinnedMessage}
	case errors.As(err, &store):
		return LocalServer{}, apperr.New(apperr.CodeSaveFailed).WithCause(store.Message).Wrap(err)
	}
	return LocalServer{}, errmap.SaveFailure(err)
}

// TestLocalServerInput names the server to try; an empty field is the saved one.
type TestLocalServerInput struct {
	BaseURL string `json:"base_url,omitempty" jsonschema:"試すアドレス。省略は保存したアドレス"`
	APIKey  string `json:"api_key,omitempty" jsonschema:"試すキー。省略は保存したキー（なければ無し）。保存はされない"`
}

// TestLocalServerOutput is the outcome of the connection test. A server that does not answer is OK false with Failure (the common error), not a tool error.
type TestLocalServerOutput struct {
	OK         bool         `json:"ok"`
	Message    string       `json:"message"`
	ModelCount int          `json:"model_count"`
	Models     []string     `json:"models" jsonschema:"サーバーが持つモデル（先頭 20 件。set_llm_assignment の model に渡す名前）"`
	Failure    *apperr.View `json:"failure,omitempty" jsonschema:"失敗したときの原因・直し方・詳細（local_llm_unreachable は接続できないとき）"`
}

func (h handlers) testLocalServer(ctx context.Context, in TestLocalServerInput) (TestLocalServerOutput, error) {
	svc := h.r.Settings.LLMLocal
	if svc == nil {
		return TestLocalServerOutput{}, Unavailable("ローカル LLM サーバーの設定")
	}
	res, err := svc.Test(ctx, llmlocal.TestInput{BaseURL: in.BaseURL, APIKey: in.APIKey})
	var invalid *llmlocal.InvalidError
	if errors.As(err, &invalid) {
		return TestLocalServerOutput{}, invalidInput(invalid.Field, invalid.Message)
	}
	if err != nil {
		return TestLocalServerOutput{}, err
	}
	out := TestLocalServerOutput{OK: res.OK, Message: res.Message, ModelCount: res.Count, Models: append([]string{}, res.Models...)}
	if res.Error != nil {
		v := res.Error.View(apperr.LangJa)
		out.Failure = &v
	}
	return out, nil
}
