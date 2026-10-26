package modelmgr

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/hfimport"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// mediaVideo is the media of a video model (localcfg.MediaVideo).
const mediaVideo = "video"

// trialUploadLimit bounds the request of a trial: the reference picture and the form around it.
const trialUploadLimit = 21 << 20

// formValue is a field of the form as the screen sends it (urlencoded or multipart).
func formValue(c *gin.Context, name string) string { return strings.TrimSpace(c.PostForm(name)) }

// trialForm is the form for a chosen model: the choices of the model select and what the model offers. want is the
// model to select ("" takes the first one that is on the bundled engine, else the first).
func (h Handlers) trialForm(want string, prev view.TrialForm) view.TrialForm {
	f := view.TrialForm{Models: []view.TrialModel{}, MaxPrompt: gentest.MaxPromptRunes, Prompt: prev.Prompt, Negative: prev.Negative, Aspect: prev.Aspect}
	models := mmview.TrialModels(h.Svc)
	var chosen *mmview.TrialModel
	for i := range models {
		if models[i].ID == want {
			chosen = &models[i]
		}
	}
	if chosen == nil && len(models) > 0 {
		chosen = &models[0]
		for i := range models {
			if models[i].Bundled {
				chosen = &models[i]
				break
			}
		}
	}
	for _, m := range models {
		f.Models = append(f.Models, view.TrialModel{ID: m.ID, Label: m.ID + "（" + kindLabel(string(m.Media)) + "）", Selected: chosen != nil && m.ID == chosen.ID})
	}
	if chosen == nil {
		return f
	}
	f.Media, f.AcceptsReference = string(chosen.Media), chosen.AcceptsReference
	if string(chosen.Media) == mediaVideo {
		f.Resolutions = chosen.Resolutions
		for _, d := range chosen.Durations {
			f.Durations = append(f.Durations, strconv.FormatFloat(d, 'f', -1, 64))
		}
	}
	return f
}

// trialPanel is the form and the list. notice and failure are the message of the form.
func (h Handlers) trialPanel(ctx context.Context, prev view.TrialForm, notice, failure string) view.TrialPanel {
	p := view.TrialPanel{Form: h.trialForm(prevModel(prev), prev), Trials: h.trials(ctx)}
	p.Form.Notice, p.Form.Error = notice, failure
	if len(p.Form.Models) == 0 {
		p.Unavailable = "試せるモデルがありません。ComfyUI エンジンとモデルを導入するか、設定の「ローカル・代替モデル」に接続先とモデルを登録してください。"
	}
	return p
}

func prevModel(prev view.TrialForm) string {
	for _, m := range prev.Models {
		if m.Selected {
			return m.ID
		}
	}
	return ""
}

// trialListSize is how many results the list shows.
const trialListSize = 20

// trials lists the latest results of the models of this screen, newest first (the generation test's history: a trial is a
// batch of one). A result that waits or runs shows that, and the list asks for itself again.
func (h Handlers) trials(ctx context.Context) view.Trials {
	if h.Gentest == nil {
		return view.Trials{}
	}
	ids := []string{}
	for _, m := range mmview.TrialModels(h.Svc) {
		ids = append(ids, m.ID)
	}
	if len(ids) == 0 {
		return view.Trials{}
	}
	recs, err := h.Gentest.History(ctx, gentest.HistoryFilter{Models: ids, Limit: trialListSize})
	if err != nil {
		return view.Trials{Message: view.Message{Error: "試し生成の一覧を読み込めませんでした。mam のログを確認してください"}}
	}
	t := view.Trials{}
	for _, r := range recs {
		it := view.Trial{
			ID: r.ID, ModelID: r.Model, Video: r.Media == gentest.MediaVideo, Prompt: r.Prompt, Reference: len(r.UsedInputs) > 0,
			Status: r.Status, Error: r.Error, CreatedAt: r.CreatedAt.Local().Format("2006-01-02 15:04"), Detail: trialDetail(r),
			Deletable: r.Status != gentest.StatusQueued && r.Status != gentest.StatusRunning,
		}
		switch r.Status {
		case gentest.StatusQueued:
			it.StatusText = "待機中"
		case gentest.StatusRunning:
			it.StatusText = "生成中"
		case gentest.StatusSucceeded:
			it.StatusText, it.Size = "完了", bytesText(r.SizeBytes)
			it.FileURL = paths.ModelManagerPart("trials", r.ID, "file")
		case gentest.StatusCancelled:
			it.StatusText = "取り消し"
		default:
			it.StatusText = "失敗"
		}
		t.Items = append(t.Items, it)
	}
	return t
}

func trialDetail(r gentest.ResultView) string {
	parts := []string{kindLabel(r.Media), "縦横比 " + r.AspectRatio}
	if r.Resolution != "" {
		parts = append(parts, r.Resolution)
	}
	if r.DurationSeconds > 0 {
		parts = append(parts, strconv.FormatFloat(r.DurationSeconds, 'f', -1, 64)+" 秒")
	}
	return strings.Join(parts, " ／ ")
}

// data is the whole screen.
func (h Handlers) data(c *gin.Context) view.Data {
	ctx := c.Request.Context()
	d := view.Data{
		Engine: engineData(ctx, h.Svc, "", false), Adult: adultData(h.Svc), Catalog: catalogData(ctx, h.Svc, nil),
		HF: hfimport.TokenData(h.Svc), HFImport: hfimport.Panel(ctx, h.Svc),
		External: view.External{Endpoints: h.externalEndpoints(c)},
		Trial:    h.trialPanel(ctx, view.TrialForm{}, "", ""),
	}
	return d
}

// Trials answers GET /model-manager/trials, which the list polls while a trial runs.
func (h Handlers) Trials(c *gin.Context) {
	render(c, http.StatusOK, view.TrialList(h.trials(c.Request.Context())))
}

// TrialForm answers GET /model-manager/trials/form (query model_id, prompt, negative_prompt, aspect_ratio): the form for
// another model, keeping what was typed.
func (h Handlers) TrialForm(c *gin.Context) {
	prev := view.TrialForm{Prompt: c.Query(view.FieldPrompt), Negative: c.Query(view.FieldNegative), Aspect: strings.TrimSpace(c.Query(view.FieldAspect))}
	render(c, http.StatusOK, view.TrialFormView(h.trialForm(c.Query(view.FieldModelID), prev)))
}

// StartTrial starts a trial generation (POST /model-manager/trials, multipart: model_id, prompt, negative_prompt,
// aspect_ratio, resolution, duration_seconds and the optional file reference) as a batch of one result of the generation test,
// and answers with the section (the list polls itself). The request is checked against the model before it is queued: 422 with
// the reason. A local model has no bill, so the estimate that is confirmed is the one of the request as the service prices it.
func (h Handlers) StartTrial(c *gin.Context) {
	ctx := c.Request.Context()
	in, prev, err := h.readTrial(c)
	if err == nil && h.Gentest == nil {
		err = modelmanager.ErrNotFound
	}
	if err == nil {
		err = h.startTrial(ctx, in)
	}
	if err != nil {
		status, msg := refuse(c, err)
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			status, msg = http.StatusRequestEntityTooLarge, "参照画像が大きすぎます"
		}
		render(c, status, view.TrialSection(h.trialPanel(ctx, prev, "", msg)))
		return
	}
	panel := h.trialPanel(ctx, prev, "試し生成を始めました。結果はこの下に並びます", "")
	panel.Form.Prompt = "" // the prompt was taken; the model and the aspect ratio stay for the next run
	render(c, http.StatusAccepted, view.TrialSection(panel))
}

// startTrial prices the request as the generation test does and runs it with that estimate.
func (h Handlers) startTrial(ctx context.Context, in trialInput) error {
	req := gentest.Request{
		Media: in.media, Models: []string{in.modelID}, Prompts: []gentest.PromptInput{{Prompt: in.prompt, Negative: in.negative}},
		AspectRatio: in.aspect, Resolution: in.resolution, DurationSeconds: in.duration,
	}
	if m, err := h.Gentest.Model(ctx, in.media, in.modelID); err == nil && len(in.picture) > 0 {
		id, _, err := h.Gentest.StageReference(in.picture)
		if err != nil {
			return err
		}
		role := gentest.RoleReference
		if m.Video != nil && m.Video.FirstFrame {
			role = gentest.RoleFirstFrame
		}
		req.References = []gentest.RefInput{{Source: gentest.FromStaged, ID: id, Role: role}}
	}
	est, err := h.Gentest.Estimate(ctx, req)
	if err != nil {
		return err
	}
	_, err = h.Gentest.Run(ctx, req, gentest.Confirmation{EstimateID: est.ID, TotalUSD: est.TotalUSD})
	return err
}

// trialInput is the trial form as the screen reads it.
type trialInput struct {
	media, modelID, prompt, negative, aspect, resolution string
	duration                                             float64
	picture                                              []byte
}

// readTrial reads the form: the input, and the form as typed (to draw it again after a refusal). A model that is not on
// the screen's list is ErrNotFound.
func (h Handlers) readTrial(c *gin.Context) (trialInput, view.TrialForm, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, trialUploadLimit)
	if err := c.Request.ParseMultipartForm(8 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		return trialInput{}, view.TrialForm{}, err
	}
	in := trialInput{
		modelID: formValue(c, view.FieldModelID), prompt: c.PostForm(view.FieldPrompt), negative: c.PostForm(view.FieldNegative),
		aspect: formValue(c, view.FieldAspect), resolution: formValue(c, view.FieldResol),
	}
	prev := view.TrialForm{Prompt: in.prompt, Negative: in.negative, Aspect: in.aspect, Models: []view.TrialModel{{ID: in.modelID, Selected: true}}}
	for _, m := range mmview.TrialModels(h.Svc) {
		if m.ID == in.modelID {
			in.media = string(m.Media)
		}
	}
	if in.media == "" {
		return in, prev, modelmanager.ErrNotFound
	}
	if d := formValue(c, view.FieldDuration); d != "" {
		v, err := strconv.ParseFloat(d, 64)
		if err != nil {
			return in, prev, &modelmanager.InvalidError{Message: "長さは秒数で指定してください"}
		}
		in.duration = v
	}
	if fh, err := c.FormFile(view.FieldReference); err == nil && fh.Size > 0 {
		f, err := fh.Open()
		if err != nil {
			return in, prev, err
		}
		defer func() { _ = f.Close() }()
		if in.picture, err = io.ReadAll(io.LimitReader(f, trialUploadLimit)); err != nil {
			return in, prev, err
		}
	}
	return in, prev, nil
}

// DeleteTrial removes a trial generation and its files (DELETE /model-manager/trials/:id). One that is running is 409.
func (h Handlers) DeleteTrial(c *gin.Context) {
	err := modelmanager.ErrNotFound
	if h.Gentest != nil {
		err = h.Gentest.DeleteResult(c.Request.Context(), c.Param("id"))
	}
	t := h.trials(c.Request.Context())
	status := http.StatusOK
	if err != nil {
		status, t.Error = refuse(c, err)
	} else {
		t.Notice = "試し生成の結果を削除しました"
	}
	render(c, status, view.TrialList(t))
}

// TrialFile serves the result of a trial (GET /model-manager/trials/:id/file) as the media routes do: the stored type,
// inside a sandbox (Content-Security-Policy: sandbox), so that the file cannot run as a page of mam.
func (h Handlers) TrialFile(c *gin.Context) {
	if h.Gentest == nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	f, err := h.Gentest.ResultFile(c.Request.Context(), c.Param("id"))
	if err != nil {
		if c.Request.Context().Err() != nil {
			return
		}
		if errors.Is(err, gterr.ErrNotFound) {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		_ = c.Error(err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	c.Header("Content-Type", f.Mime)
	c.Header("Content-Security-Policy", "sandbox")
	c.File(f.Path)
}
