package workflows

import (
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	service "github.com/ousiassllc/mam/backend/internal/service/workflows"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/workflows"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// readImport reads the API-format JSON of an import form: the uploaded file, else the pasted text.
func readImport(c *gin.Context) (name, kind, endpoint string, api []byte, err error) {
	if c.Request.ContentLength > maxImportBytes {
		return "", "", "", nil, &service.InvalidError{Message: "送信内容が大きすぎます"}
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxImportBytes)
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
		return "", "", "", nil, &service.InvalidError{Message: "送信内容を読み取れませんでした（JSON は 2 MiB までです）"}
	}
	form := c.Request.MultipartForm
	name, kind, endpoint = first(form.Value, view.FieldName), first(form.Value, view.FieldKind), first(form.Value, view.FieldEndpoint)
	if files := form.File[view.FieldAPIFile]; len(files) > 0 {
		f, err := files[0].Open()
		if err != nil {
			return "", "", "", nil, err
		}
		defer func() { _ = f.Close() }()
		data, err := io.ReadAll(io.LimitReader(f, maxImportBytes))
		return name, kind, endpoint, data, err
	}
	return name, kind, endpoint, []byte(first(form.Value, view.FieldAPIJSON)), nil
}

// Import answers POST /workflows/import (multipart: name, kind, endpoint, and api_file or api_json).
func (h Handlers) Import(c *gin.Context) {
	name, kind, endpoint, api, err := readImport(c)
	d := h.data(c.Request.Context())
	d.FormName, d.FormKind, d.FormEndpoint, d.FormJSON = name, kind, endpoint, string(api)
	if err != nil {
		h.refuse(c, err, d)
		return
	}
	if len(api) == 0 {
		h.refuse(c, &service.InvalidError{Message: "API 形式の JSON のファイルを選ぶか、貼り付けてください"}, d)
		return
	}
	w, err := h.Svc.Create(c.Request.Context(), service.CreateInput{Name: name, Kind: service.Media(kind), Endpoint: endpoint, API: api})
	if err != nil {
		h.refuse(c, err, d)
		return
	}
	h.savedOK(c, "保存しました", w)
}

// noticeOf is the confirmation of a saved workflow; a workflow that cannot run yet says what is missing.
func noticeOf(prefix string, w service.Workflow) string {
	msg := prefix + "（" + w.ModelID + "）"
	if w.Ready {
		return msg + "。画像の用途・Quality Policy・Shot の Video Model で選べます"
	}
	return msg + "。入力の割り当てがまだ足りません（「編集」で Prompt と出力のノードを選んでください）: " + strings.Join(w.Problems, " / ")
}

// Remote answers GET /workflows/remote?endpoint=<name>: the workflows the ComfyUI server holds.
func (h Handlers) Remote(c *gin.Context) {
	endpoint := strings.TrimSpace(c.Query(view.FieldEndpoint))
	d := h.data(c.Request.Context())
	d.RemoteEndpoint = endpoint
	if d.RemoteEndpoint == "" {
		d.RemoteEndpoint = service.BundledEndpoint
	}
	list, err := h.Svc.ListRemote(c.Request.Context(), endpoint)
	if err != nil {
		_, d.RemoteError = classify(c, err)
		render(c, http.StatusOK, view.Panel(d))
		return
	}
	for _, f := range list {
		d.Remote = append(d.Remote, view.RemoteFile{Path: f.Path, Name: f.Name})
	}
	render(c, http.StatusOK, view.Panel(d))
}

// ImportRemote answers POST /workflows/import/comfyui: the workflow of the ComfyUI server (its saved file and the
// API format of its last run).
func (h Handlers) ImportRemote(c *gin.Context) {
	form := postForm(c)
	d := h.data(c.Request.Context())
	d.RemoteEndpoint = first(form, view.FieldEndpoint)
	w, err := h.Svc.ImportFromComfyUI(c.Request.Context(), service.ImportRemoteInput{
		Endpoint: d.RemoteEndpoint, Path: first(form, view.FieldRemotePath), Name: first(form, view.FieldName),
		Kind: service.Media(first(form, view.FieldKind)), Replace: first(form, view.FieldReplace),
	})
	if err != nil {
		h.refuse(c, err, d)
		return
	}
	h.savedOK(c, "ComfyUI から取り込みました", w)
}

// Check answers POST /workflows/:id/check: the warnings of the connected ComfyUI, shown on the workflow's row.
func (h Handlers) Check(c *gin.Context) {
	id := c.Param("id")
	res, err := h.Svc.Check(c.Request.Context(), id)
	if err != nil {
		h.refuse(c, err, h.data(c.Request.Context()))
		return
	}
	d := h.data(c.Request.Context())
	applyCheck(&d, id, res)
	render(c, http.StatusOK, view.Panel(d))
}

// applyCheck puts what the ComfyUI lacks for the workflow onto its row.
func applyCheck(d *view.Data, id string, res service.CheckResult) {
	i := slices.IndexFunc(d.Workflows, func(r view.Row) bool { return r.ID == id })
	if i < 0 {
		return
	}
	row := &d.Workflows[i]
	row.Checked, row.CheckedOn, row.Unreachable = res.Reachable, res.Endpoint, ""
	if !res.Reachable {
		row.Unreachable = res.Reason
	}
	for _, w := range res.Warnings {
		row.Warnings = append(row.Warnings, view.Warning{Kind: w.Kind, Message: w.Message})
	}
}

// savedOK answers a save that worked: the panel with the notice, and the row of the workflow already checked against
// the connected ComfyUI (the warnings never refuse the save).
func (h Handlers) savedOK(c *gin.Context, prefix string, w service.Workflow) {
	d := h.data(c.Request.Context())
	d.Notice = noticeOf(prefix, w)
	if res, err := h.Svc.Check(c.Request.Context(), w.ID); err == nil {
		applyCheck(&d, w.ID, res)
	}
	render(c, http.StatusOK, view.Panel(d))
}

// Open answers POST /workflows/:id/open: it puts the workflow into the ComfyUI's own workflows and redirects to the
// node screen of the endpoint, which says where to open it.
func (h Handlers) Open(c *gin.Context) {
	id := c.Param("id")
	file, err := h.Svc.OpenInComfyUI(c.Request.Context(), id)
	if err != nil {
		h.refuse(c, err, h.data(c.Request.Context()))
		return
	}
	w, err := h.Svc.Get(c.Request.Context(), id)
	if err != nil {
		h.refuse(c, err, h.data(c.Request.Context()))
		return
	}
	target := paths.Nodes() + "?opened=" + queryEscape(file)
	if w.Endpoint != service.BundledEndpoint {
		target += "&endpoint=" + queryEscape(w.Endpoint)
	}
	c.Header("HX-Redirect", target)
	c.Status(http.StatusNoContent)
}

// Delete answers DELETE /workflows/:id.
func (h Handlers) Delete(c *gin.Context) {
	if err := h.Svc.Delete(c.Request.Context(), c.Param("id")); err != nil {
		h.refuse(c, err, h.data(c.Request.Context()))
		return
	}
	h.ok(c, "削除しました。過去の Take には識別子が残ります")
}

func queryEscape(s string) string {
	return strings.ReplaceAll(strings.NewReplacer("%", "%25", "&", "%26", "#", "%23", "+", "%2B", " ", "%20", "?", "%3F").Replace(s), "\n", "")
}

func atoi(s string) (int, bool) {
	if s == "" {
		return 0, true
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}
