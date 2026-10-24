package modelmgr

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
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
	f := view.TrialForm{Models: []view.TrialModel{}, MaxPrompt: modelmanager.MaxPromptRunes, Prompt: prev.Prompt, Negative: prev.Negative, Aspect: prev.Aspect}
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
func (h Handlers) trialPanel(ctx context.Context, prev view.TrialForm, notice, failure string, pending ...string) view.TrialPanel {
	p := view.TrialPanel{Form: h.trialForm(prevModel(prev), prev), Trials: h.trials(ctx, pending)}
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

// trials lists the trial generations, newest first, the running ones with their progress. pending are trial jobs the
// page was told about: one that has no record yet (queued, or about to write it) is listed as waiting, and the list asks
// for it again; one whose job ended without a record is listed as failed with the job's reason.
func (h Handlers) trials(ctx context.Context, pending []string) view.Trials {
	recs, err := h.Svc.Gentests(ctx)
	if err != nil {
		return view.Trials{Message: view.Message{Error: "試し生成の一覧を読み込めませんでした。mam のログを確認してください"}}
	}
	t := view.Trials{}
	have := map[string]bool{}
	for _, r := range recs {
		have[r.ID] = true
	}
	for _, id := range pending {
		if !have[id] {
			t.Items = append(t.Items, h.pendingTrial(ctx, id, &t))
		}
	}
	for _, r := range recs {
		it := view.Trial{
			ID: r.ID, ModelID: r.ModelID, Video: string(r.Media) == mediaVideo, Prompt: r.Prompt, Reference: r.HasReference,
			Status: string(r.Status), Error: r.Error, CreatedAt: r.CreatedAt.Local().Format("2006-01-02 15:04"), Detail: trialDetail(r),
			Deletable: r.Status != modelmanager.GentestRunning,
		}
		switch r.Status {
		case modelmanager.GentestRunning:
			it.StatusText, it.Progress = "生成中", progressOf(ctx, h.Svc, r.ID)
		case modelmanager.GentestSucceeded:
			it.StatusText, it.Size = "完了", bytesText(r.SizeBytes)
			it.FileURL = paths.ModelManagerPart("trials", r.ID, "file")
		default:
			it.StatusText = "失敗"
		}
		t.Items = append(t.Items, it)
	}
	return t
}

// pendingTrial is the list item of a trial job that has no record yet.
func (h Handlers) pendingTrial(ctx context.Context, id string, t *view.Trials) view.Trial {
	it := view.Trial{ID: id, ModelID: "（準備中）", Status: "running", StatusText: "待機中", Progress: progressOf(ctx, h.Svc, id)}
	st, err := h.Svc.JobStatus(ctx, id)
	switch {
	case err != nil:
		it.Status, it.StatusText, it.Progress = "failed", "失敗", nil
		it.Error = "この試し生成は見つかりません"
	case st.State == domain.JobFailed:
		it.Status, it.StatusText, it.Progress = "failed", "失敗", nil
		it.Error = orDefault(st.Error, "試し生成を始められませんでした")
	default:
		t.Jobs = append(t.Jobs, id)
	}
	return it
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func trialDetail(r modelmanager.GentestRecord) string {
	parts := []string{kindLabel(string(r.Media)), "縦横比 " + r.AspectRatio}
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
		Engine: engineData(ctx, h.Svc, "", false), Catalog: catalogData(ctx, h.Svc, nil), HF: hfData(h),
		External: view.External{Endpoints: h.externalEndpoints(c)},
		Trial:    h.trialPanel(ctx, view.TrialForm{}, "", ""),
	}
	return d
}

// Trials answers GET /model-manager/trials, which the list polls while a trial runs.
func (h Handlers) Trials(c *gin.Context) {
	render(c, http.StatusOK, view.TrialList(h.trials(c.Request.Context(), splitIDs(c.Query("jobs")))))
}

// TrialForm answers GET /model-manager/trials/form (query model_id, prompt, negative_prompt, aspect_ratio): the form for
// another model, keeping what was typed.
func (h Handlers) TrialForm(c *gin.Context) {
	prev := view.TrialForm{Prompt: c.Query(view.FieldPrompt), Negative: c.Query(view.FieldNegative), Aspect: strings.TrimSpace(c.Query(view.FieldAspect))}
	render(c, http.StatusOK, view.TrialFormView(h.trialForm(c.Query(view.FieldModelID), prev)))
}

// StartTrial starts a trial generation (POST /model-manager/trials, multipart: model_id, prompt, negative_prompt,
// aspect_ratio, resolution, duration_seconds and the optional file reference) as a job, and answers with the section
// (the list polls itself). The request is checked against the model before it is queued: 422 with the reason.
func (h Handlers) StartTrial(c *gin.Context) {
	ctx := c.Request.Context()
	in, prev, err := h.readTrial(c)
	var ref modelmanager.JobRef
	if err == nil {
		ref, err = h.Svc.StartGentest(ctx, in)
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
	panel := h.trialPanel(ctx, prev, "試し生成を始めました。結果はこの下に並びます", "", ref.JobID)
	panel.Form.Prompt = "" // the prompt was taken; the model and the aspect ratio stay for the next run
	render(c, http.StatusAccepted, view.TrialSection(panel))
}

// readTrial reads the form: the input of the service, and the form as typed (to draw it again after a refusal).
func (h Handlers) readTrial(c *gin.Context) (modelmanager.GentestInput, view.TrialForm, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, trialUploadLimit)
	if err := c.Request.ParseMultipartForm(8 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		return modelmanager.GentestInput{}, view.TrialForm{}, err
	}
	in := modelmanager.GentestInput{
		ModelID: formValue(c, view.FieldModelID), Prompt: c.PostForm(view.FieldPrompt), NegativePrompt: c.PostForm(view.FieldNegative),
		AspectRatio: formValue(c, view.FieldAspect), Resolution: formValue(c, view.FieldResol),
	}
	prev := view.TrialForm{Prompt: in.Prompt, Negative: in.NegativePrompt, Aspect: in.AspectRatio, Models: []view.TrialModel{{ID: in.ModelID, Selected: true}}}
	if d := formValue(c, view.FieldDuration); d != "" {
		v, err := strconv.ParseFloat(d, 64)
		if err != nil {
			return in, prev, &modelmanager.InvalidError{Message: "長さは秒数で指定してください"}
		}
		in.DurationSeconds = v
	}
	if fh, err := c.FormFile(view.FieldReference); err == nil && fh.Size > 0 {
		f, err := fh.Open()
		if err != nil {
			return in, prev, err
		}
		defer func() { _ = f.Close() }()
		data, err := io.ReadAll(io.LimitReader(f, trialUploadLimit))
		if err != nil {
			return in, prev, err
		}
		in.Reference = &modelmanager.Reference{Data: data, MimeType: fh.Header.Get("Content-Type")}
	}
	return in, prev, nil
}

// DeleteTrial removes a trial generation and its files (DELETE /model-manager/trials/:id). One that is running is 409.
func (h Handlers) DeleteTrial(c *gin.Context) {
	err := h.Svc.DeleteGentest(c.Request.Context(), c.Param("id"))
	t := h.trials(c.Request.Context(), nil)
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
	path, mime, err := h.Svc.GentestFile(c.Request.Context(), c.Param("id"))
	if err != nil {
		if c.Request.Context().Err() != nil {
			return
		}
		if errors.Is(err, modelmanager.ErrNotFound) {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		_ = c.Error(err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	c.Header("Content-Type", mime)
	c.Header("Content-Security-Policy", "sandbox")
	c.File(path)
}
