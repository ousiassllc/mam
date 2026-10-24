package localmodels

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	service "github.com/ousiassllc/mam/backend/internal/service/localmodels"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/localmodels"
)

const endpointExists = "その名前の接続先はすでにあります。別の名前にしてください（名前は後から変えられません）"

// NewEndpoint answers GET /settings/local-models/endpoints/new with the form that adds an endpoint.
func (h Handlers) NewEndpoint(c *gin.Context) {
	h.open(c, view.Editor{Endpoint: &view.EndpointForm{Kind: view.KindComfyUI}})
}

// EditEndpoint answers GET /settings/local-models/endpoints/:name/edit with the form that changes the
// endpoint's address, concurrency and timeout (its name and kind are shown, not fields).
func (h Handlers) EditEndpoint(c *gin.Context) {
	e, found, err := h.endpointOf(c.Param("name"))
	if err == nil && !found {
		err = service.ErrNotFound
	}
	if err != nil {
		h.refuse(c, err, "", view.Editor{})
		return
	}
	h.open(c, view.Editor{Endpoint: &view.EndpointForm{
		Edit: true, Name: e.Name, Kind: string(e.Kind), BaseURL: e.BaseURL,
		MaxConcurrency: intField(e.MaxConcurrency), TimeoutSeconds: intField(e.TimeoutSeconds),
	}})
}

// CreateEndpoint registers the endpoint of the form (POST /settings/local-models/endpoints; fields named by
// view.FieldName, FieldKind, FieldBaseURL, FieldConcurrency, FieldTimeout and the optional FieldAPIKey) and
// answers with the panel: 200, or 409 / 422 with the reason and the form as submitted (the key is never
// shown again, so it has to be typed again).
func (h Handlers) CreateEndpoint(c *gin.Context) {
	f := endpointForm(postForm(c), false, "", "")
	in, err := endpointInput(f)
	if err == nil {
		err = h.Local.CreateEndpoint(c.Request.Context(), in)
	}
	if err != nil {
		h.refuse(c, err, endpointExists, view.Editor{Endpoint: &f})
		return
	}
	if key := strings.TrimSpace(postForm(c).Get(view.FieldAPIKey)); key != "" {
		if err := h.Local.SetEndpointKey(in.Name, key); err != nil {
			status, msg := h.classify(c, err, "")
			d := SectionData(h.Local)
			d.Error = "接続先「" + in.Name + "」は追加しましたが、API キーを保存できませんでした: " + msg
			h.respond(c, status, d, true)
			return
		}
	}
	h.ok(c, "接続先「"+in.Name+"」を追加しました。すぐに使えます")
}

// UpdateEndpoint changes the address, concurrency and timeout of the endpoint called :name (PUT
// /settings/local-models/endpoints/:name). The name and the kind are not read from the form: they are fixed.
func (h Handlers) UpdateEndpoint(c *gin.Context) {
	name := c.Param("name")
	kind, err := h.kindOf(name)
	if err != nil {
		h.refuse(c, err, "", view.Editor{})
		return
	}
	f := endpointForm(postForm(c), true, name, kind)
	in, err := endpointInput(f)
	if err == nil {
		in.Name, in.Kind = "", "" // an update never renames and never changes the kind
		err = h.Local.UpdateEndpoint(c.Request.Context(), name, in)
	}
	if err != nil {
		h.refuse(c, err, "", view.Editor{Endpoint: &f})
		return
	}
	h.ok(c, "接続先「"+name+"」を保存しました。すぐに反映されます")
}

// DeleteEndpoint removes the endpoint with its models and key (DELETE /settings/local-models/endpoints/:name).
// An endpoint whose model an image purpose or a Quality Policy uses is 422 with where.
func (h Handlers) DeleteEndpoint(c *gin.Context) {
	name := c.Param("name")
	if err := h.Local.DeleteEndpoint(c.Request.Context(), name); err != nil {
		h.refuse(c, err, "", view.Editor{})
		return
	}
	h.ok(c, "接続先「"+name+"」とそのモデルを削除しました")
}

// TestEndpoint tests the saved endpoint called :name (POST /settings/local-models/endpoints/:name/test) and
// answers with the result line. A test that fails is 200 with the reason: the request itself worked.
func (h Handlers) TestEndpoint(c *gin.Context) {
	res, err := h.Local.TestEndpoint(c.Request.Context(), c.Param("name"))
	h.testResult(c, res, err)
}

// TestDraft tests the endpoint of the form as typed, saved or not (POST /settings/local-models/endpoints/test;
// the fields of the endpoint form). An empty key tests with the saved key of an endpoint that has one.
func (h Handlers) TestDraft(c *gin.Context) {
	form := postForm(c)
	in := service.TestInput{
		Name: strings.TrimSpace(form.Get(view.FieldName)), Kind: service.Kind(form.Get(view.FieldKind)),
		BaseURL: strings.TrimSpace(form.Get(view.FieldBaseURL)), APIKey: form.Get(view.FieldAPIKey),
	}
	if in.Kind == "" || in.BaseURL == "" {
		h.testResult(c, service.TestResult{}, &service.InvalidError{Message: "種類と base_url を入力してください"})
		return
	}
	res, err := h.Local.Test(c.Request.Context(), in)
	h.testResult(c, res, err)
}

func (h Handlers) testResult(c *gin.Context, res service.TestResult, err error) {
	status, v := http.StatusOK, view.TestView{OK: res.OK, Message: res.Message, Note: res.Note, Models: res.Models}
	if err != nil {
		status, v.Message = h.classify(c, err, "")
	}
	render(c, status, view.TestResult(v))
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
	render(c, status, view.KeyRow(name, view.Key{Configured: key.Configured, LastChars: key.LastChars, Error: msg}))
}
