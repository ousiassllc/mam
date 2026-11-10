// Package loraimport holds the handlers of "LoRA を追加" on the model manager screen (Issue #1119; spec/api/endpoints-model-manager.md): the file the
// browser sends is checked (inspect), completed by the user (what the file says of itself is only the starting point of the form) and added to the
// managed loras folder (commit); the list can be edited and cleared. Every rule is modelmanager.Service's (LoRAs); the screen only asks.
package loraimport

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/lorafile"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/loraimport"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/loraimport"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// bodySlack is room in the request for the form fields and the boundaries around the file.
const bodySlack = 1 << 20

// idleTimeout is how long an upload may stall between two reads (the server's ReadTimeout is an absolute deadline that a large file can exceed).
var idleTimeout = time.Minute

// Handlers answer the routes of the section.
type Handlers struct {
	svc *modelmanager.Service
}

// Register adds the routes of the section to the engine.
func Register(engine *gin.Engine, svc *modelmanager.Service) {
	h := Handlers{svc: svc}
	engine.POST(paths.ModelManagerPart("loras", "inspect"), h.Inspect)
	engine.POST(paths.ModelManagerPart("loras", "discard"), h.Discard)
	engine.POST(paths.ModelManagerPart("loras"), h.Commit)
	engine.PUT(paths.ModelManagerPart("loras"), h.Update)
	engine.DELETE(paths.ModelManagerPart("loras"), h.Delete)
}

// Section is the section on the first draw of the screen.
func Section(svc *modelmanager.Service) templ.Component {
	return view.Section(PanelOf(svc, nil, "", ""), ListOf(svc, "", ""))
}

// refuse is the status and message of an error of the service: 422 for an input that cannot be taken, 409 when the state does not allow it, 404 for
// something that is not there; anything else is a fault (500, logged, with a generic message).
func refuse(c *gin.Context, err error) (int, string) {
	if errors.Is(err, loraimport.ErrNotFound) {
		return http.StatusNotFound, "対象が見つかりません。画面を読み込み直してください"
	}
	switch kind, msg := mmview.Refuse(err); kind {
	case mmview.RefusalInvalid:
		return http.StatusUnprocessableEntity, msg
	case mmview.RefusalConflict:
		return http.StatusConflict, msg
	case mmview.RefusalNotFound:
		return http.StatusNotFound, msg
	}
	_ = c.Error(err)
	return http.StatusInternalServerError, "処理できませんでした。mam のログを確認してください"
}

func render(c *gin.Context, status int, component templ.Component) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	if err := component.Render(c.Request.Context(), c.Writer); err != nil {
		_ = c.Error(err)
		if !c.Writer.Written() {
			c.AbortWithStatus(http.StatusInternalServerError)
		}
	}
}

// idleBody gives every read of the upload a fresh read deadline.
type idleBody struct {
	io.ReadCloser
	rc *http.ResponseController
}

func (b idleBody) Read(p []byte) (int, error) {
	_ = b.rc.SetReadDeadline(time.Now().Add(idleTimeout)) // not every writer supports deadlines
	return b.ReadCloser.Read(p)
}

// Inspect answers POST /model-manager/loras/inspect (multipart, field file): the file is streamed to the imports folder and checked, and the answer is
// the form that completes it (or the choose-a-file form with the reason it was refused). Nothing is placed in the loras folder yet.
func (h Handlers) Inspect(c *gin.Context) {
	ctx := c.Request.Context()
	limit := lorafile.MaxBytes + bodySlack
	if c.Request.ContentLength > limit {
		render(c, http.StatusRequestEntityTooLarge, view.PanelView(PanelOf(h.svc, nil, "", tooLarge())))
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, idleBody{ReadCloser: c.Request.Body, rc: http.NewResponseController(c.Writer)}, limit)
	st, err := h.stageFirstFile(ctx, c)
	if err != nil {
		status, msg := refuse(c, err)
		var tl *http.MaxBytesError
		if errors.As(err, &tl) {
			status, msg = http.StatusRequestEntityTooLarge, tooLarge()
		}
		render(c, status, view.PanelView(PanelOf(h.svc, nil, "", msg)))
		return
	}
	render(c, http.StatusOK, view.PanelView(PanelOf(h.svc, &st, "", "")))
}

func tooLarge() string {
	return "ファイルが大きすぎます（上限 " + fetch.HumanBytes(uint64(lorafile.MaxBytes)) + "）。LoRA ではなくチェックポイントなどの大きなモデルではありませんか"
}

// stageFirstFile reads the multipart body as a stream (no temporary copy) up to the part "file" and stages it.
func (h Handlers) stageFirstFile(ctx context.Context, c *gin.Context) (loraimport.Staged, error) {
	mr, err := c.Request.MultipartReader()
	if err != nil {
		return loraimport.Staged{}, invalidInput("multipart/form-data で、フィールド file にファイルを入れて送ってください")
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return loraimport.Staged{}, invalidInput("ファイルが選ばれていません")
		}
		if err != nil {
			return loraimport.Staged{}, err
		}
		if part.FormName() != view.FieldFile || part.FileName() == "" {
			_ = part.Close()
			continue
		}
		return h.svc.LoRAs.Stage(ctx, rawFileName(part), declaredSize(c.Request.ContentLength), partReader{part})
	}
}

// rawFileName is the file name as the browser sent it. multipart.Part.FileName cuts the directories off ("../../x" becomes "x"), which would hide
// a name that tries to leave the folder: the importer is to see it and refuse it.
func rawFileName(part *multipart.Part) string {
	if _, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition")); err == nil {
		if name, ok := params["filename"]; ok {
			return name
		}
	}
	return part.FileName()
}

// partReader hides the part's other methods: the importer reads a stream.
type partReader struct{ *multipart.Part }

// declaredSize is the size of the file the request announces, as far as it can be told: the length of the body without the room the boundaries and fields
// take (0 when the request does not say).
func declaredSize(contentLength int64) int64 {
	if contentLength <= bodySlackForSize {
		return 0
	}
	return contentLength - bodySlackForSize
}

// bodySlackForSize is more than the boundaries and the one field of the form take.
const bodySlackForSize = 4 << 10

func invalidInput(msg string) error { return &modelmanager.InvalidError{Message: msg} }

// Discard answers POST /model-manager/loras/discard (field token): the file that waits is thrown away.
func (h Handlers) Discard(c *gin.Context) {
	h.svc.LoRAs.Discard(c.PostForm(view.FieldToken))
	render(c, http.StatusOK, view.PanelView(PanelOf(h.svc, nil, "", "")))
}

// Commit answers POST /model-manager/loras (fields token, name, base_models, strength, trigger_words, license_name, commercial): the file is placed in the
// loras folder and recorded. The answer is the choose-a-file form with the confirmation, and the list drawn out of band. A refusal comes back as a message
// above the fields (HX-Retarget), where the user is looking, and the form keeps what was typed.
func (h Handlers) Commit(c *gin.Context) {
	meta, err := metaOf(c)
	if err == nil {
		var m loraimport.Model
		m, err = h.svc.LoRAs.Commit(c.Request.Context(), loraimport.Input{Token: c.PostForm(view.FieldToken), Name: strings.TrimSpace(c.PostForm(view.FieldName)), Meta: meta})
		if err == nil {
			p := PanelOf(h.svc, nil, "「"+m.Name+"」を追加しました。単発生成の「LoRA と強さ」で選べます。", "")
			render(c, http.StatusCreated, templ.Join(view.PanelView(p), listOOB(h.svc)))
			return
		}
	}
	status, msg := refuse(c, err)
	if _, ok := h.svc.LoRAs.Staged(c.PostForm(view.FieldToken)); !ok {
		render(c, status, view.PanelView(PanelOf(h.svc, nil, "", msg))) // nothing waits any more: start again
		return
	}
	c.Header("HX-Retarget", "#"+view.MsgID)
	c.Header("HX-Reswap", "innerHTML")
	render(c, status, templ.Raw(templ.EscapeString(msg)))
}

// Update answers PUT /model-manager/loras?path= with the same fields (without token and name): what the user confirmed about a LoRA is replaced.
func (h Handlers) Update(c *gin.Context) {
	meta, err := metaOf(c)
	if err == nil {
		_, err = h.svc.LoRAs.Update(c.Request.Context(), c.Query(view.FieldPath), meta)
	}
	if err != nil {
		status, msg := refuse(c, err)
		render(c, status, view.ListView(ListOf(h.svc, "", msg)))
		return
	}
	render(c, http.StatusOK, view.ListView(ListOf(h.svc, "保存しました", "")))
}

// Delete answers DELETE /model-manager/loras?path=: the file and its record go; a file nobody recorded is never touched.
func (h Handlers) Delete(c *gin.Context) {
	if err := h.svc.LoRAs.Delete(c.Request.Context(), c.Query(view.FieldPath)); err != nil {
		status, msg := refuse(c, err)
		render(c, status, view.ListView(ListOf(h.svc, "", msg)))
		return
	}
	render(c, http.StatusOK, view.ListView(ListOf(h.svc, "削除しました", "")))
}

// metaOf reads the fields of what is known of the LoRA. An empty strength is "not set".
func metaOf(c *gin.Context) (loraimport.Meta, error) {
	m := loraimport.Meta{
		BaseModels: c.PostFormArray(view.FieldBase), TriggerWords: c.PostForm(view.FieldTriggers),
		LicenseName: c.PostForm(view.FieldLicense), Commercial: c.PostForm(view.FieldCommercial),
	}
	if raw := strings.TrimSpace(c.PostForm(view.FieldStrength)); raw != "" {
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return m, invalidInput("推奨の強さは数字で入力してください（0.05〜1）")
		}
		m.Strength = &v
	}
	return m, nil
}
