package localmodels

import (
	"context"
	"errors"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
)

func registerEndpoints(r *mcpkit.Registrar, h handlers) {
	mcpkit.Read(r, "list_local_endpoints",
		"登録済みの接続先（ComfyUI・OpenAI 互換）の一覧。API キーは設定済みかと末尾数文字だけ。--model-config で固定されていれば pinned / Registered ComfyUI / OpenAI-compatible endpoints with their models. Never returns an API key.",
		h.listEndpoints)
	mcpkit.Read(r, "get_local_endpoint",
		"接続先を 1 件取得する / One endpoint with the identifiers of its models.",
		h.getEndpoint)
	mcpkit.Write(r, "create_local_endpoint",
		"接続先を登録する。kind は comfyui（画像・動画）か openai-compat（画像のみ）。base_url には http・localhost・LAN・tailnet のアドレスを使える（リンクローカルは不可）。名前と kind は後から変えられない。保存するとすぐに効く / Register an endpoint (effective at once). name and kind are fixed for good.",
		h.createEndpoint)
	mcpkit.Write(r, "update_local_endpoint",
		"接続先の base_url・同時実行数・タイムアウトを変える（名前と kind は変えられない。省略した数は既定に戻る）。すぐに効く / Change the address, concurrency and timeout of an endpoint.",
		h.updateEndpoint, mcpkit.Idempotent())
	mcpkit.Destroy(r, "delete_local_endpoint",
		"接続先と、その接続先のモデル、API キーを削除する。モデルが画像の用途・Quality Policy に割り当てられていれば拒否される（先に set_model_assignment で変える）/ Delete an endpoint with its models and key; refused while a model is assigned.",
		h.deleteEndpoint)
	mcpkit.Read(r, "test_local_endpoint",
		"接続テスト。name だけなら保存済みの接続先を、kind と base_url を渡せば未保存の接続先を試す（ComfyUI は /system_stats のバージョンと GPU、OpenAI 互換は /v1/models の件数）。到達できないのは ok=false の結果で、エラーではない / Connection test of a saved endpoint (name) or of a draft (kind + base_url).",
		h.testEndpoint, mcpkit.OpenWorld())
	mcpkit.Write(r, "set_local_endpoint_key",
		"接続先の API キーを保存・置き換える（任意。OS のセキュアストレージ。固定ファイルでも可）。応答にキーは含まれない / Store the API key of an endpoint (never echoed).",
		h.setKey, mcpkit.Idempotent())
	mcpkit.Destroy(r, "clear_local_endpoint_key",
		"接続先の API キーを削除する（未設定でもエラーにならない）/ Remove the API key of an endpoint.",
		h.clearKey, mcpkit.Idempotent())
}

// KeyState is whether an endpoint has an API key. The key itself is never returned.
type KeyState struct {
	Configured  bool   `json:"configured"`
	LastChars   string `json:"last_chars,omitempty" jsonschema:"キー末尾の数文字（設定済みのときだけ。短いキーでは空）"`
	Unavailable bool   `json:"unavailable,omitempty" jsonschema:"OS のキーストアを読めず、キーの有無が分からない（接続先の一覧は返る）"`
}

// Endpoint is a registered endpoint.
type Endpoint struct {
	Name           string   `json:"name"`
	Kind           string   `json:"kind" jsonschema:"comfyui or openai-compat"`
	BaseURL        string   `json:"base_url"`
	MaxConcurrency int      `json:"max_concurrency" jsonschema:"同時実行数（既定を反映した値）"`
	TimeoutSeconds int      `json:"timeout_seconds" jsonschema:"1 回の呼び出しのタイムアウト秒（既定を反映した値）"`
	APIKey         KeyState `json:"api_key"`
	Models         []string `json:"models" jsonschema:"この接続先のモデルの識別子（<kind>:<endpoint>/<name>）"`
}

// ListEndpointsOutput is every endpoint.
type ListEndpointsOutput struct {
	Pinned    bool       `json:"pinned" jsonschema:"--model-config で設定ファイルが固定されていて、接続先とモデルは変更できない（API キーと接続テストは使える）"`
	Endpoints []Endpoint `json:"endpoints"`
}

func (h handlers) listEndpoints(_ context.Context, _ Input) (ListEndpointsOutput, error) {
	svc, err := h.local()
	if err != nil {
		return ListEndpointsOutput{}, err
	}
	v, err := svc.View()
	if err != nil {
		return ListEndpointsOutput{}, mapError(err)
	}
	out := ListEndpointsOutput{Pinned: v.Pinned, Endpoints: make([]Endpoint, 0, len(v.Endpoints))}
	for _, e := range v.Endpoints {
		out.Endpoints = append(out.Endpoints, endpointOf(e, v.Models))
	}
	return out, nil
}

func endpointOf(e localmodels.EndpointView, models []localmodels.ModelView) Endpoint {
	out := Endpoint{
		Name: e.Name, Kind: string(e.Kind), BaseURL: e.BaseURL, MaxConcurrency: e.MaxConcurrency, TimeoutSeconds: e.TimeoutSeconds,
		APIKey: KeyState{Configured: e.Key.Configured, LastChars: e.Key.LastChars, Unavailable: e.Key.Unavailable}, Models: []string{},
	}
	for _, m := range models {
		if m.Endpoint == e.Name {
			out.Models = append(out.Models, m.ID)
		}
	}
	return out
}

// EndpointName names an endpoint.
type EndpointName struct {
	Name string `json:"name" jsonschema:"接続先の名前（list_local_endpoints の name）"`
}

// find reads the saved endpoint called name.
func (h handlers) findEndpoint(name string) (Endpoint, error) {
	svc, err := h.local()
	if err != nil {
		return Endpoint{}, err
	}
	v, err := svc.View()
	if err != nil {
		return Endpoint{}, mapError(err)
	}
	i := slices.IndexFunc(v.Endpoints, func(e localmodels.EndpointView) bool { return e.Name == name })
	if i < 0 {
		return Endpoint{}, localmodels.ErrNotFound
	}
	return endpointOf(v.Endpoints[i], v.Models), nil
}

func (h handlers) getEndpoint(_ context.Context, in EndpointName) (Endpoint, error) {
	e, err := h.findEndpoint(in.Name)
	return e, mapError(err)
}

// CreateEndpointInput is an endpoint to register.
type CreateEndpointInput struct {
	Name           string `json:"name" jsonschema:"英小文字・数字・. _ - の 1〜63 文字。識別子の一部になり、後から変えられない"`
	Kind           string `json:"kind" jsonschema:"comfyui（画像・動画）または openai-compat（画像のみ）。後から変えられない"`
	BaseURL        string `json:"base_url" jsonschema:"例: http://127.0.0.1:8188、http://192.168.0.10:1234、http://100.101.102.103:8188。リンクローカルは不可"`
	MaxConcurrency int    `json:"max_concurrency,omitempty" jsonschema:"同時実行数。省略で 1"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"タイムアウト秒。省略で ComfyUI 3600・互換 900"`
	APIKey         string `json:"api_key,omitempty" jsonschema:"任意の API キー。応答には含まれない"`
}

func (h handlers) createEndpoint(ctx context.Context, in CreateEndpointInput) (Endpoint, error) {
	svc, err := h.local()
	if err != nil {
		return Endpoint{}, err
	}
	err = svc.CreateEndpoint(ctx, localmodels.EndpointInput{
		Name: in.Name, Kind: localcfg.Kind(in.Kind), BaseURL: in.BaseURL, MaxConcurrency: in.MaxConcurrency, TimeoutSeconds: in.TimeoutSeconds,
	})
	if err != nil {
		return Endpoint{}, mapError(err)
	}
	if in.APIKey != "" {
		if err := svc.SetEndpointKey(in.Name, in.APIKey); err != nil {
			var invalid *localmodels.InvalidError
			if errors.As(err, &invalid) {
				return Endpoint{}, &service.ValidationError{Field: "api_key", Message: "接続先は登録しましたが、API キーを保存できませんでした: " + invalid.Message}
			}
			return Endpoint{}, err
		}
	}
	e, err := h.findEndpoint(in.Name)
	return e, mapError(err)
}

// UpdateEndpointInput is the new address, concurrency and timeout of an endpoint.
type UpdateEndpointInput struct {
	Name           string `json:"name" jsonschema:"変える接続先の名前（名前と kind は変えられない）"`
	BaseURL        string `json:"base_url"`
	MaxConcurrency int    `json:"max_concurrency,omitempty" jsonschema:"省略で 1"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"省略で ComfyUI 3600・互換 900"`
}

func (h handlers) updateEndpoint(ctx context.Context, in UpdateEndpointInput) (Endpoint, error) {
	svc, err := h.local()
	if err != nil {
		return Endpoint{}, err
	}
	err = svc.UpdateEndpoint(ctx, in.Name, localmodels.EndpointInput{BaseURL: in.BaseURL, MaxConcurrency: in.MaxConcurrency, TimeoutSeconds: in.TimeoutSeconds})
	if err != nil {
		return Endpoint{}, mapError(err)
	}
	e, err := h.findEndpoint(in.Name)
	return e, mapError(err)
}

// Deleted names what was deleted.
type Deleted struct {
	Name string `json:"name"`
}

func (h handlers) deleteEndpoint(ctx context.Context, in EndpointName) (Deleted, error) {
	svc, err := h.local()
	if err != nil {
		return Deleted{}, err
	}
	if err := svc.DeleteEndpoint(ctx, in.Name); err != nil {
		return Deleted{}, mapError(err)
	}
	return Deleted(in), nil
}

// TestInput is the endpoint to test: a saved one by name, or a draft by kind and base_url.
type TestInput struct {
	Name    string `json:"name,omitempty" jsonschema:"保存済みの接続先の名前。kind・base_url と一緒に渡すと、その名前の保存済みキーで未保存の設定を試す"`
	Kind    string `json:"kind,omitempty" jsonschema:"未保存の接続先を試すときの種類"`
	BaseURL string `json:"base_url,omitempty" jsonschema:"未保存の接続先を試すときの base_url"`
	APIKey  string `json:"api_key,omitempty" jsonschema:"未保存の接続先を試すときのキー（保存しない）"`
}

// TestOutput is the outcome of a connection test.
type TestOutput struct {
	OK      bool     `json:"ok" jsonschema:"期待どおりの種類のサーバーが答えた"`
	Message string   `json:"message"`
	Note    string   `json:"note,omitempty" jsonschema:"失敗ではない注意（/v1/models が無い、古い ComfyUI など）"`
	Version string   `json:"version,omitempty" jsonschema:"ComfyUI のバージョン"`
	Models  []string `json:"models,omitempty" jsonschema:"OpenAI 互換サーバーが返したモデル ID（20 件まで）"`
}

func (h handlers) testEndpoint(ctx context.Context, in TestInput) (TestOutput, error) {
	svc, err := h.local()
	if err != nil {
		return TestOutput{}, err
	}
	if in.Name == "" && (in.Kind == "" || in.BaseURL == "") {
		return TestOutput{}, &service.ValidationError{Message: "name（保存済み）か、kind と base_url（未保存）を指定してください"}
	}
	if in.Name != "" && in.Kind == "" && in.BaseURL == "" && in.APIKey != "" {
		return TestOutput{}, &service.ValidationError{Field: "api_key", Message: "保存済みの接続先は保存済みのキーで試します。別のキーで試すには kind と base_url も指定してください"}
	}
	res, err := svc.Test(ctx, localmodels.TestInput{Name: in.Name, Kind: localcfg.Kind(in.Kind), BaseURL: in.BaseURL, APIKey: in.APIKey})
	if err != nil {
		return TestOutput{}, mapError(err)
	}
	return TestOutput{OK: res.OK, Message: res.Message, Note: res.Note, Version: res.Version, Models: res.Models}, nil
}

// KeyOutput is the key of an endpoint after the change; the key itself is never returned.
type KeyOutput struct {
	Name string `json:"name"`
	KeyState
}

// SetKeyInput is the key to store.
type SetKeyInput struct {
	Name   string `json:"name"`
	APIKey string `json:"api_key" jsonschema:"保存する API キー。応答には含まれない"`
}

func (h handlers) setKey(_ context.Context, in SetKeyInput) (KeyOutput, error) {
	svc, err := h.local()
	if err != nil {
		return KeyOutput{}, err
	}
	if err := svc.SetEndpointKey(in.Name, in.APIKey); err != nil {
		return KeyOutput{}, mapError(err)
	}
	return h.keyOf(in.Name)
}

func (h handlers) clearKey(_ context.Context, in EndpointName) (KeyOutput, error) {
	svc, err := h.local()
	if err != nil {
		return KeyOutput{}, err
	}
	if err := svc.ClearEndpointKey(in.Name); err != nil {
		return KeyOutput{}, mapError(err)
	}
	return h.keyOf(in.Name)
}

func (h handlers) keyOf(name string) (KeyOutput, error) {
	st, err := h.svc.LocalModels.EndpointKeyStatus(name)
	if err != nil {
		return KeyOutput{}, mapError(err)
	}
	return KeyOutput{Name: name, KeyState: KeyState{Configured: st.Configured, LastChars: st.LastChars}}, nil
}
