package localmodels_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func TestSection_IsOnTheSettingsScreenWithEmptyState(t *testing.T) {
	s := newScreen(t, false, "")
	body := s.page()

	assert.Contains(t, body, `id="local-models-panel"`)
	assert.Contains(t, body, "ローカル・代替モデル")
	assert.Contains(t, body, `data-testid="local-models-empty"`)
	assert.Contains(t, body, `hx-get="`+paths.LocalEndpoints()+`/new"`, "the add button opens the form")
	assert.NotContains(t, body, `data-testid="local-models-pinned"`)
	assert.NotContains(t, body, `id="model-assignment"`, "the assignment of the models is a category of its own")
}

func TestEndpoint_CreateShowsItAndSavesTheFileAndTheKeyAppearsAsStatusOnly(t *testing.T) {
	s := newScreen(t, false, "")
	const secret = "sk-local-secret-1234567890"

	rec := s.do(http.MethodPost, paths.LocalEndpoints(), url.Values{
		"name": {"gpu-box"}, "kind": {"comfyui"}, "base_url": {"http://100.101.102.103:8188"}, "max_concurrency": {"2"}, "timeout_seconds": {"1800"}, "api_key": {secret},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, "接続先「gpu-box」を追加しました")
	assert.Contains(t, body, `data-endpoint="gpu-box"`)
	assert.Contains(t, body, "http://100.101.102.103:8188")
	assert.Contains(t, body, "同時実行 2")
	assert.Contains(t, body, "1800 秒")
	assert.Contains(t, body, "設定済み（末尾 7890）")
	assert.NotContains(t, body, secret, "the key is never rendered")
	assert.NotContains(t, s.page(), secret)
	assert.NotContains(t, s.savedFile(), secret, "the key is not in models.json")
	assert.Contains(t, s.savedFile(), `"base_url": "http://100.101.102.103:8188"`)

	stored, err := s.keys.Get("endpoint:gpu-box")
	require.NoError(t, err)
	assert.Equal(t, secret, stored)
	assert.Contains(t, s.page(), `data-endpoint="gpu-box"`, "it is on the screen after a reload")
}

func TestEndpoint_Refusals(t *testing.T) {
	s := newScreen(t, false, "")
	require.Equal(t, http.StatusOK, s.addEndpoint("box", "comfyui", "http://127.0.0.1:8188").Code)
	before := s.savedFile()

	dup := s.addEndpoint("box", "openai-compat", "http://127.0.0.1:1234")
	assert.Equal(t, http.StatusConflict, dup.Code)
	assert.Contains(t, dup.Body.String(), "その名前の接続先はすでにあります")
	assert.Contains(t, dup.Body.String(), `data-testid="local-endpoint-form"`, "the form stays open with what was typed")

	linkLocal := s.addEndpoint("meta", "openai-compat", "http://169.254.169.254")
	assert.Equal(t, http.StatusUnprocessableEntity, linkLocal.Code)
	assert.Contains(t, linkLocal.Body.String(), `data-testid="local-form-error"`)
	assert.Contains(t, linkLocal.Body.String(), `value="http://169.254.169.254"`, "the typed address is shown again")

	badName := s.addEndpoint("Bad Name", "comfyui", "http://127.0.0.1:8188")
	assert.Equal(t, http.StatusUnprocessableEntity, badName.Code)

	notNumber := s.do(http.MethodPost, paths.LocalEndpoints(), url.Values{"name": {"x"}, "kind": {"comfyui"}, "base_url": {"http://127.0.0.1:8188"}, "max_concurrency": {"many"}})
	assert.Equal(t, http.StatusUnprocessableEntity, notNumber.Code)
	assert.Contains(t, notNumber.Body.String(), "同時実行数は整数で入力してください")

	assert.Equal(t, before, s.savedFile(), "a refused input writes nothing")
	assert.NotContains(t, s.page(), `data-endpoint="meta"`)
}

func TestEndpoint_EditCannotRenameOrChangeTheKind(t *testing.T) {
	s := newScreen(t, false, "")
	require.Equal(t, http.StatusOK, s.addEndpoint("box", "comfyui", "http://127.0.0.1:8188").Code)

	edit := s.do(http.MethodGet, paths.LocalEndpoint("box", "edit"), nil)
	require.Equal(t, http.StatusOK, edit.Code)
	body := edit.Body.String()
	assert.Contains(t, body, `data-testid="local-endpoint-fixed"`, "the name and the kind are shown")
	fields := fieldNames(t, body, "local-endpoint-form")
	assert.ElementsMatch(t, []string{"base_url", "max_concurrency", "timeout_seconds"}, fields, "the name and the kind are not fields of an edit")
	assert.NotContains(t, fields, "api_key", "the key has its own form")

	// A hand-made request that carries a new name and kind changes neither.
	rec := s.do(http.MethodPut, paths.LocalEndpoint("box"), url.Values{
		"name": {"renamed"}, "kind": {"openai-compat"}, "base_url": {"http://127.0.0.1:9000"}, "max_concurrency": {"3"}, "timeout_seconds": {"120"},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "http://127.0.0.1:9000")
	assert.Contains(t, rec.Body.String(), `data-endpoint="box"`)
	assert.NotContains(t, rec.Body.String(), "renamed")
	assert.Contains(t, s.savedFile(), `"kind": "comfyui"`)

	missing := s.do(http.MethodPut, paths.LocalEndpoint("nope"), url.Values{"base_url": {"http://127.0.0.1:9000"}})
	assert.Equal(t, http.StatusNotFound, missing.Code)
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodGet, paths.LocalEndpoint("nope", "edit"), nil).Code)
}

func TestEndpoint_DeleteRemovesItsModelsAndKeyButNotWhileAModelIsChosen(t *testing.T) {
	s := newScreen(t, false, "")
	require.Equal(t, http.StatusOK, s.addEndpoint("box", "openai-compat", "http://127.0.0.1:1234").Code)
	require.Equal(t, http.StatusOK, s.do(http.MethodPut, paths.LocalEndpoint("box", "key"), url.Values{"api_key": {"abcdefgh"}}).Code)
	require.Equal(t, http.StatusOK, s.do(http.MethodPost, paths.LocalEndpoint("box", "models"),
		url.Values{"name": {"flux"}, "media": {"image"}, "model_id": {"flux-schnell"}}).Code)

	// Choose the model for a purpose, then try to delete the endpoint.
	assign := s.assignImage("new_reference", "image_new_reference_primary_model", "openai-compat:box/flux")
	require.Equal(t, http.StatusOK, assign.Code, assign.Body.String())
	refused := s.do(http.MethodDelete, paths.LocalEndpoint("box"), nil)
	assert.Equal(t, http.StatusUnprocessableEntity, refused.Code)
	assert.Contains(t, refused.Body.String(), "images.new_reference.primary")
	assert.Contains(t, s.page(), `data-endpoint="box"`)

	// Free the model; now the delete works and takes the key with it.
	require.Equal(t, http.StatusOK, s.do(http.MethodDelete, paths.ModelSettings(), nil).Code)
	rec := s.do(http.MethodDelete, paths.LocalEndpoint("box"), nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "接続先「box」とそのモデルを削除しました")
	assert.Contains(t, rec.Body.String(), `data-testid="local-models-empty"`)
	assert.Empty(t, s.savedFile(), "the file is removed with its last entries")
	_, err := s.keys.Get("endpoint:box")
	assert.Error(t, err, "the key is deleted with the endpoint")
}

func TestEndpointKey_SetAndClearAnswerWithTheKeyRowOnly(t *testing.T) {
	s := newScreen(t, false, "")
	require.Equal(t, http.StatusOK, s.addEndpoint("box", "openai-compat", "http://127.0.0.1:1234").Code)

	rec := s.do(http.MethodPut, paths.LocalEndpoint("box", "key"), url.Values{"api_key": {"  key-with-tail-WXYZ  "}})
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "設定済み（末尾 WXYZ）")
	assert.NotContains(t, body, "key-with-tail", "never the key")
	assert.NotContains(t, body, `id="local-models-panel"`, "only the row: the other fields keep what was typed")
	assert.Contains(t, body, `hx-delete="`+paths.LocalEndpoint("box", "key")+`"`)

	bad := s.do(http.MethodPut, paths.LocalEndpoint("box", "key"), url.Values{"api_key": {"has space inside"}})
	assert.Equal(t, http.StatusUnprocessableEntity, bad.Code)
	assert.Contains(t, bad.Body.String(), `data-testid="local-key-error"`)
	assert.Contains(t, bad.Body.String(), "設定済み（末尾 WXYZ）", "the stored key is untouched")

	cleared := s.do(http.MethodDelete, paths.LocalEndpoint("box", "key"), nil)
	require.Equal(t, http.StatusOK, cleared.Code)
	assert.Contains(t, cleared.Body.String(), "未設定")
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodPut, paths.LocalEndpoint("nope", "key"), url.Values{"api_key": {"abcdefgh"}}).Code)
}

func TestConnectionTest_ComfyUICompatAndLinkLocal(t *testing.T) {
	comfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/system_stats" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"system":{"comfyui_version":"0.3.60"},"devices":[{"name":"cuda:0 NVIDIA GeForce RTX 4090"}]}`))
	}))
	t.Cleanup(comfy.Close)
	compat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"flux-schnell"},{"id":"sdxl"}]}`))
	}))
	t.Cleanup(compat.Close)
	bare := httptest.NewServer(http.NotFoundHandler()) // answers, but has no /v1/models
	t.Cleanup(bare.Close)

	s := newScreen(t, false, "")
	require.Equal(t, http.StatusOK, s.addEndpoint("box", "comfyui", comfy.URL).Code)
	require.Equal(t, http.StatusOK, s.addEndpoint("lm", "openai-compat", compat.URL).Code)
	require.Equal(t, http.StatusOK, s.addEndpoint("bare", "openai-compat", bare.URL).Code)

	rec := s.do(http.MethodPost, paths.LocalEndpoint("box", "test"), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "ComfyUI に接続できました（バージョン 0.3.60）")
	assert.Contains(t, rec.Body.String(), "NVIDIA GeForce RTX 4090", "the GPU is shown")

	rec = s.do(http.MethodPost, paths.LocalEndpoint("lm", "test"), nil)
	assert.Contains(t, rec.Body.String(), "モデル 2 件")
	assert.Contains(t, rec.Body.String(), "flux-schnell、sdxl")

	rec = s.do(http.MethodPost, paths.LocalEndpoint("bare", "test"), nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "サーバーに到達できました")
	assert.Contains(t, rec.Body.String(), "/v1/models は提供されていません")

	// A draft is tested without saving it; link-local is refused with a plain message.
	draft := s.do(http.MethodPost, paths.LocalEndpoints()+"/test", url.Values{"kind": {"comfyui"}, "base_url": {comfy.URL}})
	assert.Contains(t, draft.Body.String(), "ComfyUI に接続できました")
	meta := s.do(http.MethodPost, paths.LocalEndpoints()+"/test", url.Values{"kind": {"comfyui"}, "base_url": {"http://169.254.169.254"}})
	assert.Equal(t, http.StatusUnprocessableEntity, meta.Code)
	assert.Contains(t, meta.Body.String(), "リンクローカル")
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodPost, paths.LocalEndpoint("nope", "test"), nil).Code)
	assert.Contains(t, s.page(), `data-endpoint="box"`)
	assert.NotContains(t, s.page(), "NVIDIA", "a test result is not part of the screen")
}

func TestConnectionTest_AFailureIsTheResultNotAnError(t *testing.T) {
	down := httptest.NewServer(http.NotFoundHandler())
	url := down.URL
	down.Close()
	s := newScreen(t, false, "")
	require.Equal(t, http.StatusOK, s.addEndpoint("gone", "comfyui", url).Code)

	rec := s.do(http.MethodPost, paths.LocalEndpoint("gone", "test"), nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "接続できませんでした")
}

func TestEndpointList_ABrokenKeyStoreDoesNotHideTheEndpoints(t *testing.T) {
	s := newScreen(t, false, "")
	require.Equal(t, http.StatusOK, s.addEndpoint("box", "comfyui", "http://127.0.0.1:8188").Code)
	s.keys.FailWith(errors.New("no secret service"))

	body := s.page()
	assert.Contains(t, body, `data-endpoint="box"`, "the endpoint is listed")
	assert.Contains(t, body, "キーストアを利用できません")
	assert.NotContains(t, body, `data-testid="local-models-problem"`)
	// The other actions work on what is saved.
	assert.Equal(t, http.StatusOK, s.do(http.MethodGet, paths.LocalEndpoint("box", "edit"), nil).Code)
	assert.Equal(t, http.StatusOK, s.do(http.MethodGet, paths.LocalEndpoint("box", "models", "new"), nil).Code)
}
