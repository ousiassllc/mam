package local_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func TestEndpointModal_AddAndEditAreModalsThatTestTheDraft(t *testing.T) {
	s := newScreen(t, false, "")
	require.Equal(t, http.StatusOK, s.addEndpoint("box", "comfyui", "http://127.0.0.1:8188").Code)

	add := s.do(http.MethodGet, paths.LocalEndpoints()+"/new", nil)
	require.Equal(t, http.StatusOK, add.Code)
	body := add.Body.String()
	assert.Contains(t, body, `<h2 id="mam-modal-title"`)
	assert.Contains(t, body, `hx-post="`+paths.LocalEndpoints()+`"`)
	assert.Contains(t, body, `hx-post="`+paths.LocalEndpoints()+`/test"`, "the connection test of what is typed")
	assert.Contains(t, body, `hx-vals="{}"`, "the connection test sends an object: htmx cannot read null and sends nothing")
	assert.Contains(t, body, `method="post"`, "a key never goes into an address even if the browser sends the form first")
	assert.Equal(t, 1, strings.Count(body, "autofocus"), "the name is where the focus starts")
	assert.Contains(t, fieldNames(t, body, "local-endpoint-form"), "api_key")

	edit := s.do(http.MethodGet, paths.LocalEndpoint("box", "edit")+"?page=1", nil)
	require.Equal(t, http.StatusOK, edit.Code)
	assert.Contains(t, fieldNames(t, edit.Body.String(), "local-endpoint-form"), "page", "the modal of the page of the endpoint says so")
	plain := s.do(http.MethodGet, paths.LocalEndpoint("box", "edit"), nil)
	assert.NotContains(t, fieldNames(t, plain.Body.String(), "local-endpoint-form"), "page")
}

func TestEndpointModal_ARequestThatIsNotHTMXGetsThePageOfTheCategory(t *testing.T) {
	s := newScreen(t, false, "")
	require.Equal(t, http.StatusOK, s.addEndpoint("box", "comfyui", "http://127.0.0.1:8188").Code)

	for _, target := range []string{paths.LocalEndpoints() + "/new", paths.LocalEndpoint("box", "edit")} {
		page := s.get(target)
		assert.Contains(t, page, `<nav aria-label="設定"`, target)
		assert.Contains(t, page, `id="local-models-panel"`, target)
		assert.Contains(t, page, `<link rel="canonical" href="`+paths.LocalModels()+`"`, target)
		assert.NotContains(t, page, `data-testid="local-endpoint-form"`, target)
	}
	for _, target := range []string{paths.LocalEndpoint("box", "models", "new"), paths.LocalEndpoint("box")} {
		assert.Contains(t, s.get(target), `id="local-endpoint-page"`, target)
	}
}

func TestEndpointSave_AnswersTheCardOutOfBandAndClosesTheModal(t *testing.T) {
	s := newScreen(t, false, "")

	created := s.addEndpoint("box", "comfyui", "http://127.0.0.1:8188")
	require.Equal(t, http.StatusOK, created.Code)
	assert.Equal(t, "modal-close", created.Header().Get("HX-Trigger-After-Swap"))
	assert.Contains(t, created.Body.String(), `hx-swap-oob="beforeend:#local-endpoints"`, "a new card is added to the list")
	assert.Contains(t, created.Body.String(), `<p id="local-endpoints-empty" hx-swap-oob="delete">`, "and the sentence that there is none goes")
	assert.Contains(t, created.Body.String(), `role="status"`, "the toast says what happened")
	assert.NotContains(t, created.Body.String(), `id="local-models-panel"`, "the whole category is not drawn again")

	form := url.Values{"base_url": {"http://127.0.0.1:9000"}}
	list := s.do(http.MethodPut, paths.LocalEndpoint("box"), form)
	require.Equal(t, http.StatusOK, list.Code)
	assert.Equal(t, "modal-close", list.Header().Get("HX-Trigger-After-Swap"))
	assert.Contains(t, list.Body.String(), `id="local-endpoint-box"`)
	assert.Contains(t, list.Body.String(), `hx-swap-oob="true"`)
	assert.Contains(t, list.Body.String(), `hx-delete="`+paths.LocalEndpoint("box")+`"`, "a card of the list can be deleted")

	form.Set("page", "1")
	page := s.do(http.MethodPut, paths.LocalEndpoint("box"), form)
	require.Equal(t, http.StatusOK, page.Code)
	assert.NotContains(t, page.Body.String(), `hx-delete="`+paths.LocalEndpoint("box")+`"`, "the summary on the page of the endpoint has no delete: it is what the page is about")
	assert.NotContains(t, page.Body.String(), `モデル（`, "and no link to the page it is on")
}

func TestEndpointDelete_AnswersToastsAndBringsTheEmptySentenceBackWithTheLastOne(t *testing.T) {
	s := newScreen(t, false, "")
	require.Equal(t, http.StatusOK, s.addEndpoint("a", "comfyui", "http://127.0.0.1:8188").Code)
	require.Equal(t, http.StatusOK, s.addEndpoint("b", "comfyui", "http://127.0.0.1:8189").Code)

	first := s.do(http.MethodDelete, paths.LocalEndpoint("a"), nil)
	require.Equal(t, http.StatusOK, first.Code)
	assert.Contains(t, first.Body.String(), "接続先「a」とそのモデルを削除しました")
	assert.NotContains(t, first.Body.String(), `hx-swap-oob="beforebegin:#local-endpoints"`, "one is still there")

	last := s.do(http.MethodDelete, paths.LocalEndpoint("b"), nil)
	assert.Contains(t, last.Body.String(), `hx-swap-oob="beforebegin:#local-endpoints"`)
	assert.Contains(t, last.Body.String(), `data-testid="local-models-empty"`)

	gone := s.do(http.MethodDelete, paths.LocalEndpoint("b"), nil)
	assert.Equal(t, http.StatusNotFound, gone.Code)
	assert.Equal(t, "none", gone.Header().Get("HX-Reswap"), "a refusal is a toast and leaves the page as it is")
}

func TestEndpointPage_ListsTheModelsAndIsAPageOfItsOwn(t *testing.T) {
	s := newScreen(t, false, "")
	require.Equal(t, http.StatusOK, s.addEndpoint("lm", "openai-compat", "http://127.0.0.1:1234").Code)

	page := s.get(paths.LocalEndpoint("lm"))
	assert.Contains(t, page, `<title>設定: ローカル・代替モデル: lm`, "every page has a title of its own")
	assert.Contains(t, page, `href="`+paths.LocalModels()+`"`, "the way back")
	assert.Contains(t, page, `aria-current="page"`, "the category is the current one of the navigation")
	assert.Contains(t, page, `id="local-models-empty"`, "no model yet")
	assert.NotContains(t, page, `<link rel="canonical"`, "it is its own address")

	fragment := s.do(http.MethodGet, paths.LocalEndpoint("lm"), nil)
	assert.Equal(t, http.StatusOK, fragment.Code)
	assert.NotContains(t, fragment.Body.String(), "<html", "an HTMX request gets the body only")
	assert.Contains(t, fragment.Body.String(), `id="local-endpoint-page"`)
	assert.Equal(t, http.StatusNotFound, s.getRec(paths.LocalEndpoint("nope")).Code)

	// The first model takes the "no models" row away, the last one brings it back.
	added := s.do(http.MethodPost, paths.LocalEndpoint("lm", "models"), url.Values{"name": {"flux"}, "media": {"image"}, "model_id": {"flux-schnell"}})
	require.Equal(t, http.StatusOK, added.Code)
	assert.Contains(t, added.Body.String(), `<span id="local-models-empty" hx-swap-oob="delete">`)
	assert.Contains(t, s.get(paths.LocalEndpoint("lm")), `id="local-model-lm-flux"`)

	changed := s.do(http.MethodPut, paths.LocalEndpoint("lm", "models", "flux"), url.Values{"media": {"image"}, "model_id": {"flux-dev"}})
	require.Equal(t, http.StatusOK, changed.Code)
	assert.True(t, strings.HasPrefix(changed.Body.String(), `<template><tr id="local-model-lm-flux"`), "a table row survives the parser only inside a template: %s", changed.Body.String())
	assert.Contains(t, changed.Body.String(), `hx-swap-oob="true"`)

	removed := s.do(http.MethodDelete, paths.LocalEndpoint("lm", "models", "flux"), nil)
	require.Equal(t, http.StatusOK, removed.Code)
	assert.True(t, strings.HasPrefix(removed.Body.String(), `<tr id="local-models-empty"`), "the rows that are left come first (the button swaps them into the table): %s", removed.Body.String())
	assert.Contains(t, removed.Body.String(), `モデル 0 件`, "the summary of the endpoint is replaced with the new count")
}

func TestEndpointNamedNew_IsListedWithoutAPageAndItsFormsStillOpen(t *testing.T) {
	const content = `{"endpoints":{"new":{"kind":"comfyui","base_url":"http://127.0.0.1:8188"}}}`
	s := newScreen(t, false, content)

	body := s.page()
	assert.Contains(t, body, `data-endpoint="new"`)
	assert.Contains(t, body, `data-testid="local-endpoint-no-page"`, "the card says why its models cannot be opened")
	assert.NotContains(t, body, `href="`+paths.LocalEndpoint("new")+`"`, "there is no link to a page that the form of the add button hides")
	assert.Contains(t, body, `hx-delete="`+paths.LocalEndpoint("new")+`"`, "it can be deleted and made again under another name")

	add := s.do(http.MethodGet, paths.LocalEndpoints()+"/new", nil)
	assert.Contains(t, add.Body.String(), `hx-post="`+paths.LocalEndpoints()+`"`, "the address of the add form is the add form")
	edit := s.do(http.MethodGet, paths.LocalEndpoint("new", "edit"), nil)
	assert.Contains(t, edit.Body.String(), `hx-put="`+paths.LocalEndpoint("new")+`"`)
	refused := s.addEndpoint("new", "comfyui", "http://127.0.0.1:8189")
	assert.Equal(t, http.StatusUnprocessableEntity, refused.Code)
	assert.Contains(t, refused.Body.String(), "「new」は接続先の名前に使えません")
}
