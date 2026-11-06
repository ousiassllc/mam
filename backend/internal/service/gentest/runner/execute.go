package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/direct"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/apperr/errmap"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/refs"
	"github.com/ousiassllc/mam/backend/internal/textutil"
)

// execute makes one result with the direct call of its model and settles it.
func (br *batchRun) execute(ctx context.Context, r gentestrepo.Result) {
	s := br.s
	var p record.ResultParams
	_ = json.Unmarshal(r.Params, &p)
	if s.HiddenWith != nil && s.HiddenWith(r.Model, p.Exposed) { // the values name a file of an adult model and the gate is closed
		s.settle(ctx, r.ID, gentestrepo.StatusFailed, ReasonHidden, nil)
		return
	}
	var (
		file, mime string
		size       int64
		cost       *float64
		err        error
	)
	switch {
	case p.VideoEnhance != nil:
		file, mime, size, err = br.enhanceVideo(ctx, r, p)
	case r.Media == gentestrepo.MediaVideo:
		file, mime, size, cost, err = br.video(ctx, r, p)
	case p.FaceSwap != nil:
		file, mime, size, err = br.faceSwap(ctx, r, p)
	default:
		file, mime, size, cost, err = br.image(ctx, r, p)
	}
	switch {
	case err == nil:
		s.settle(ctx, r.ID, gentestrepo.StatusSucceeded, "", func(st *gentestrepo.Settlement) {
			st.FilePath, st.MimeType, st.SizeBytes, st.ActualCost = path.Join(r.BatchID, file), mime, size, cost
		})
	case ctx.Err() != nil && !isInterruptedAtTopaz(err): // a job Topaz accepted has its own sentence (the request id to look it up by)
		s.settle(ctx, r.ID, gentestrepo.StatusFailed, ReasonInterrupted, nil)
	default:
		s.Logger.Warn("generation test result failed", "result_id", r.ID, "model", r.Model, "err", err)
		s.failureSettle(ctx, r, s.failure(r, err))
	}
}

// inputsOf are the images the result was given, in the order of the request.
func (br *batchRun) inputsOf(r gentestrepo.Result) []loaded {
	var out []loaded
	for _, id := range r.UsedInputs {
		if in, ok := br.inputs[id]; ok {
			out = append(out, in)
		}
	}
	slices.SortFunc(out, func(a, b loaded) int { return a.position - b.position })
	return out
}

func (br *batchRun) image(ctx context.Context, r gentestrepo.Result, p record.ResultParams) (file, mime string, size int64, cost *float64, err error) {
	req := modeladapter.ImageRequest{Prompt: r.SentPrompt, AspectRatio: p.AspectRatio, Seed: seedOf(r)}
	if r.NegativeMode == gentestrepo.NegativeField {
		req.NegativePrompt = sentNegative(r, p)
		if p.AvoidPose {
			req.NegativePrompt = refs.WithAvoidPose(req.NegativePrompt)
		}
	}
	roles := modelcatalog.ReferenceCapsOf(br.s.Adapter.Config(), domain.ModelName(r.Model)).Roles
	for _, in := range br.inputsOf(r) {
		if in.role == gentestrepo.RoleMarkup { // laid over the first of the other inputs, not one of them (GPT Image 2.6)
			markup := in.in
			req.Markup = &markup
			continue
		}
		role, strength := refs.InputOf(roles, in.role, in.strength)
		req.Inputs = append(req.Inputs, in.forRole(role))
		req.Options.Refs = append(req.Options.Refs, localcfg.RefOption{Role: role, Strength: strength})
	}
	if p.LoRA != nil {
		sent := p.LoRA.Sent()
		req.Options.LoRA = &sent
	}
	if p.Sampling != nil {
		req.Options.Sampling = *p.Sampling
	}
	if p.GPT != nil {
		req.GPT = *p.GPT
	}
	req.Options.Exposed, req.Options.SpeedMode = p.Exposed, p.SpeedMode
	if p.Inpaint != nil {
		if in, ok := br.inputs[p.Inpaint.MaskInput]; ok {
			req.Inpaint = &modeladapter.Inpaint{Mask: in.in, Denoise: p.Inpaint.Denoise}
		}
	}
	if p.Upscale != nil {
		req.Options.UpscaleScale = float64(p.Upscale.Scale)
	}
	out, err := direct.Image(ctx, br.s.Adapter, domain.ModelName(r.Model), req)
	if err != nil {
		return "", "", 0, nil, err
	}
	file = r.ID + extensionFor(out.MimeType)
	if err := fsutil.WriteFileAtomic(filepath.Join(br.dir, file), out.Data, 0o600); err != nil {
		return "", "", 0, nil, fmt.Errorf("store the image: %w", err)
	}
	if isRaster(out.MimeType) { // an SVG or a GIF has no face to compare: it is not scored, and not failed
		br.scoreFace(ctx, r, out.Data)
	}
	return file, out.MimeType, int64(len(out.Data)), out.ActualCost, nil
}

func (br *batchRun) video(ctx context.Context, r gentestrepo.Result, p record.ResultParams) (file, mime string, size int64, cost *float64, err error) {
	req := modeladapter.VideoRequest{
		Prompt: r.SentPrompt, DurationSeconds: p.DurationSeconds, Resolution: p.Resolution, AspectRatio: p.AspectRatio, Seed: seedOf(r), SpeedMode: p.SpeedMode,
	}
	if r.NegativeMode == gentestrepo.NegativeField {
		req.NegativePrompt = sentNegative(r, p)
	}
	if p.Sampling != nil {
		req.Sampling = *p.Sampling
	}
	for _, in := range br.inputsOf(r) {
		img := in.in
		switch in.role {
		case gentestrepo.RoleFirstFrame:
			req.FirstFrame = &img
		case gentestrepo.RoleLastFrame:
			req.LastFrame = &img
		default:
			req.References = append(req.References, img)
		}
	}
	out, err := direct.Video(ctx, br.s.Adapter, modeladapter.VideoTarget{Model: domain.ModelName(r.Model), Resolution: p.Resolution}, req)
	if err != nil {
		return "", "", 0, nil, err
	}
	file = r.ID + extensionFor(out.MimeType)
	size = out.File.Size()
	if err := out.File.StoreAs(filepath.Join(br.dir, file), 0o600); err != nil {
		return "", "", 0, nil, fmt.Errorf("store the video: %w", err)
	}
	return file, out.MimeType, size, out.ActualCost, nil
}

func seedOf(r gentestrepo.Result) *int64 {
	if r.SeedSent {
		return r.Seed
	}
	return nil
}

// extensionFor is the extension of a stored result file.
func extensionFor(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/svg+xml":
		return ".svg"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "video/quicktime":
		return ".mov"
	}
	return ".png"
}

// isRaster says the image is one a face detector reads (PNG, JPEG, WebP): not a GIF (GPT Image 2.6's animation) or an SVG (its drawing).
func isRaster(mime string) bool {
	return mime == "image/png" || mime == "image/jpeg" || mime == "image/webp"
}

// maxErrorRunes bounds the sentence of a failure kept in the history.
const maxErrorRunes = 600

// failure is the failure of a call as the common error (#1010): what the app itself refused (Explain), else what errmap knows of the
// outside services; any other is unknown, and is logged with the correlation id the error carries.
func (s *Runner) failure(r gentestrepo.Result, err error) *apperr.Error {
	if s.Explain != nil {
		if e := s.Explain(err); e != nil {
			return e
		}
	}
	return errmap.Report(s.Logger, "generation test result failed with an unknown error", err, "result_id", r.ID, "model", r.Model)
}

// failureSettle settles r as failed with e: the sentence of it in Japanese (the column the history lines show) and the common error (what
// the detail and the tools read).
func (s *Runner) failureSettle(ctx context.Context, r gentestrepo.Result, e *apperr.Error) {
	line := textutil.TruncateRunes(e.View(apperr.LangJa).Line(apperr.LangJa), maxErrorRunes)
	s.settle(ctx, r.ID, gentestrepo.StatusFailed, line, func(st *gentestrepo.Settlement) { st.ErrorInfo = e.Marshal() })
}

// sentNegative is the negative prompt a result sends in the model's own field: the English one when it was translated.
func sentNegative(r gentestrepo.Result, p record.ResultParams) string {
	if p.Translation != nil {
		return p.Translation.Negative
	}
	return r.NegativePrompt
}
