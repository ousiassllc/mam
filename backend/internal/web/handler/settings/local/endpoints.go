package local

import (
	"errors"
	"net/http"
	"strings"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	service "github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modal"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/local"
)

const endpointExists = "その名前の接続先はすでにあります。別の名前にしてください（名前は後から変えられません）"

// NewEndpoint answers GET /settings/local-models/endpoints/new with the modal that adds an endpoint. A pinned file has no
// modal: 409 says why.
func (h Handlers) NewEndpoint(c *gin.Context) {
	if !settingshttp.IsHTMX(c) {
		h.Category(c)
		return
	}
	settingshttp.NoStore(c)
	if status, msg := h.unchangeable(); status != 0 {
		settingshttp.Render(c, status, modal.Notice(formTitle(view.EndpointForm{}), msg))
		return
	}
	settingshttp.Render(c, http.StatusOK, view.EndpointModal(view.EndpointForm{Kind: view.KindComfyUI}, ""))
}

// EditEndpoint answers GET /settings/local-models/endpoints/:name/edit with the modal that changes the endpoint's address,
// concurrency and timeout (its name and kind are shown, not fields). With the query view.PageQuery the modal was opened on
// the page of the endpoint, and its save replaces the summary there.
func (h Handlers) EditEndpoint(c *gin.Context) {
	if !settingshttp.IsHTMX(c) {
		h.Category(c)
		return
	}
	settingshttp.NoStore(c)
	e, found := h.endpointOf(c.Param("name"))
	if status, msg := h.unchangeable(); status != 0 {
		settingshttp.Render(c, status, modal.Notice(formTitle(view.EndpointForm{Edit: true}), msg))
		return
	}
	if !found {
		settingshttp.Render(c, http.StatusNotFound, modal.Notice(formTitle(view.EndpointForm{Edit: true}), notFound))
		return
	}
	settingshttp.Render(c, http.StatusOK, view.EndpointModal(view.EndpointForm{
		Edit: true, OnPage: c.Query(view.PageQuery) == "1", Name: e.Name, Kind: e.Kind, BaseURL: e.BaseURL,
		MaxConcurrency: intField(e.MaxConcurrency), TimeoutSeconds: intField(e.TimeoutSeconds),
	}, ""))
}

// CreateEndpoint registers the endpoint of the modal's form (POST /settings/local-models/endpoints; fields named by
// view.FieldName, FieldKind, FieldBaseURL, FieldConcurrency, FieldTimeout and the optional FieldAPIKey). It answers with
// the card of the new endpoint added to the list out of band, a toast and the event that closes the modal; a refusal is 409
// or 422 and the modal again with the reason and the form as submitted (the key is never shown again, so it has to be
// typed again).
func (h Handlers) CreateEndpoint(c *gin.Context) {
	settingshttp.NoStore(c)
	f := endpointForm(postForm(c), false, "", "")
	in, err := endpointInput(f)
	if err == nil {
		err = h.Local.CreateEndpoint(c.Request.Context(), in)
	}
	if err != nil {
		h.refuse(c, err, endpointExists, f)
		return
	}
	toast := atoms.SuccessToastOOB("接続先「" + in.Name + "」を追加しました。すぐに使えます")
	status := http.StatusOK
	if key := strings.TrimSpace(postForm(c).Get(view.FieldAPIKey)); key != "" {
		if err := h.Local.SetEndpointKey(in.Name, key); err != nil {
			var msg string
			status, msg = h.classify(c, err, "")
			toast = atoms.ToastOOB("接続先「" + in.Name + "」は追加しましたが、API キーを保存できませんでした: " + msg)
		}
	}
	e, _ := h.endpointOf(in.Name)
	settingshttp.CloseModal(c)
	settingshttp.Render(c, status, templ.Join(view.EndpointAppended(e), view.EmptyGone(), toast))
}

// UpdateEndpoint changes the address, concurrency and timeout of the endpoint called :name (PUT
// /settings/local-models/endpoints/:name). The name and the kind are not read from the form: they are fixed. It answers with
// the card of the endpoint replaced out of band, a toast and the event that closes the modal.
func (h Handlers) UpdateEndpoint(c *gin.Context) {
	settingshttp.NoStore(c)
	name := c.Param("name")
	kind, err := h.kindOf(name)
	if err != nil {
		h.refuse(c, err, "", view.EndpointForm{Edit: true})
		return
	}
	f := endpointForm(postForm(c), true, name, kind)
	in, err := endpointInput(f)
	if err == nil {
		in.Name, in.Kind = "", "" // an update never renames and never changes the kind
		err = h.Local.UpdateEndpoint(c.Request.Context(), name, in)
	}
	if err != nil {
		h.refuse(c, err, "", f)
		return
	}
	e, _ := h.endpointOf(name)
	settingshttp.CloseModal(c)
	settingshttp.Render(c, http.StatusOK, templ.Join(view.EndpointCardOOB(e, f.OnPage), atoms.SuccessToastOOB("接続先「"+name+"」を保存しました。すぐに反映されます")))
}

// DeleteEndpoint removes the endpoint with its models and key (DELETE /settings/local-models/endpoints/:name) and answers
// with the cards that are left (the button swaps them into the list), a toast and, when there are none, the sentence that
// says so. An endpoint whose model an image purpose or a Quality Policy uses is 422 with where, as a toast: the card stays.
func (h Handlers) DeleteEndpoint(c *gin.Context) {
	name := c.Param("name")
	if err := h.Local.DeleteEndpoint(c.Request.Context(), name); err != nil {
		status, msg := h.classify(c, err, "")
		settingshttp.Toast(c, status, msg)
		return
	}
	d := SectionData(h.Local)
	parts := []templ.Component{view.EndpointCards(d), atoms.SuccessToastOOB("接続先「" + name + "」とそのモデルを削除しました")}
	if len(d.Endpoints) == 0 {
		parts = append(parts, view.EmptyNotice())
	}
	settingshttp.Render(c, http.StatusOK, templ.Join(parts...))
}

// TestEndpoint tests the saved endpoint called :name (POST /settings/local-models/endpoints/:name/test) and
// answers with the result line. A test that fails is 200 with the reason: the request itself worked.
func (h Handlers) TestEndpoint(c *gin.Context) {
	res, err := h.Local.TestEndpoint(c.Request.Context(), c.Param("name"))
	h.testResult(c, res, err, view.TestResult)
}

// TestDraft tests the endpoint of the form as typed, saved or not (POST /settings/local-models/endpoints/test;
// the fields of the endpoint form). An empty key tests with the saved key of an endpoint that has one.
func (h Handlers) TestDraft(c *gin.Context) {
	settingshttp.NoStore(c)
	form := postForm(c)
	in := service.TestInput{
		Name: strings.TrimSpace(form.Get(view.FieldName)), Kind: service.Kind(form.Get(view.FieldKind)),
		BaseURL: strings.TrimSpace(form.Get(view.FieldBaseURL)), APIKey: form.Get(view.FieldAPIKey),
	}
	if in.Kind == "" || in.BaseURL == "" {
		h.testResult(c, service.TestResult{}, &service.InvalidError{Message: "種類と接続先の URL を入力してください"}, view.DraftTestResult)
		return
	}
	res, err := h.Local.Test(c.Request.Context(), in)
	h.testResult(c, res, err, view.DraftTestResult)
}

// testResult answers with the line of a connection test, drawn by render.
func (h Handlers) testResult(c *gin.Context, res service.TestResult, err error, render func(view.TestView) templ.Component) {
	status, v := http.StatusOK, view.TestView{OK: res.OK, Message: res.Message, Note: res.Note, Models: res.Models}
	if err != nil {
		status, v.Message = h.classify(c, err, "")
	}
	settingshttp.Render(c, status, render(v))
}

// SetKey stores the API key of the endpoint called :name (PUT /settings/local-models/endpoints/:name/key,
// field api_key) and answers with the key row: configured, and the last characters, never the key. It works
// on a pinned file too (the key is not in the file).
func (h Handlers) SetKey(c *gin.Context) {
	name := c.Param("name")
	h.keyRow(c, name, h.Local.SetEndpointKey(name, postForm(c).Get(view.FieldAPIKey)))
}

// ClearKey removes the API key of the endpoint called :name (DELETE .../key) and answers with the key row.
func (h Handlers) ClearKey(c *gin.Context) {
	name := c.Param("name")
	h.keyRow(c, name, h.Local.ClearEndpointKey(name))
}

func (h Handlers) keyRow(c *gin.Context, name string, err error) {
	status, msg := http.StatusOK, ""
	if err != nil {
		status, msg = h.classify(c, err, "")
	}
	key, kerr := h.Local.EndpointKeyStatus(name)
	if kerr != nil && !errors.Is(kerr, service.ErrNotFound) {
		_ = c.Error(kerr)
	}
	settingshttp.Render(c, status, view.KeyRow(name, view.Key{Configured: key.Configured, LastChars: key.LastChars, Error: msg}))
}
