package gentest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/direct"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/refs"
	"github.com/ousiassllc/mam/backend/internal/textutil"
)

// execute makes one result with the direct call of its model and settles it.
func (br *batchRun) execute(ctx context.Context, r gentestrepo.Result) {
	s := br.s
	var p resultParams
	_ = json.Unmarshal(r.Params, &p)
	var (
		file, mime string
		size       int64
		cost       *float64
		err        error
	)
	if r.Media == gentestrepo.MediaVideo {
		file, mime, size, cost, err = br.video(ctx, r, p)
	} else {
		file, mime, size, cost, err = br.image(ctx, r, p)
	}
	switch {
	case err == nil:
		s.settle(ctx, r.ID, gentestrepo.StatusSucceeded, "", func(st *gentestrepo.Settlement) {
			st.FilePath, st.MimeType, st.SizeBytes, st.ActualCost = path.Join(r.BatchID, file), mime, size, cost
		})
	case ctx.Err() != nil:
		s.settle(ctx, r.ID, gentestrepo.StatusFailed, ReasonInterrupted, nil)
	default:
		s.Logger.Warn("generation test result failed", "result_id", r.ID, "model", r.Model, "err", err)
		s.settle(ctx, r.ID, gentestrepo.StatusFailed, s.failureText(ctx, err), nil)
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

func (br *batchRun) image(ctx context.Context, r gentestrepo.Result, p resultParams) (file, mime string, size int64, cost *float64, err error) {
	req := modeladapter.ImageRequest{Prompt: r.SentPrompt, AspectRatio: p.AspectRatio, Seed: seedOf(r)}
	if r.NegativeMode == gentestrepo.NegativeField {
		req.NegativePrompt = sentNegative(r, p)
		if p.AvoidPose {
			req.NegativePrompt = refs.WithAvoidPose(req.NegativePrompt)
		}
	}
	roles := modelcatalog.ReferenceCapsOf(br.s.Adapter.Config(), domain.ModelName(r.Model)).Roles
	for _, in := range br.inputsOf(r) {
		role, strength := refs.InputOf(roles, in.role, in.strength)
		req.Inputs = append(req.Inputs, in.in)
		req.Options.Refs = append(req.Options.Refs, localcfg.RefOption{Role: role, Strength: strength})
	}
	if p.LoRA != nil {
		req.Options.LoRA = &localcfg.LoRA{Name: p.LoRA.Name, Strength: p.LoRA.Strength}
	}
	out, err := direct.Image(ctx, br.s.Adapter, domain.ModelName(r.Model), req)
	if err != nil {
		return "", "", 0, nil, err
	}
	file = r.ID + extensionFor(out.MimeType)
	if err := fsutil.WriteFileAtomic(filepath.Join(br.dir, file), out.Data, 0o600); err != nil {
		return "", "", 0, nil, fmt.Errorf("store the image: %w", err)
	}
	return file, out.MimeType, int64(len(out.Data)), out.ActualCost, nil
}

func (br *batchRun) video(ctx context.Context, r gentestrepo.Result, p resultParams) (file, mime string, size int64, cost *float64, err error) {
	req := modeladapter.VideoRequest{
		Prompt: r.SentPrompt, DurationSeconds: p.DurationSeconds, Resolution: p.Resolution, AspectRatio: p.AspectRatio, Seed: seedOf(r),
	}
	if r.NegativeMode == gentestrepo.NegativeField {
		req.NegativePrompt = sentNegative(r, p)
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
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "video/quicktime":
		return ".mov"
	}
	return ".png"
}

// maxErrorRunes bounds the text of a failure kept in the history.
const maxErrorRunes = 400

// failureText is what the history says about a failed result: a known failure in words, any other as the error says it.
func (s *Service) failureText(ctx context.Context, err error) string {
	var msg string
	explained := ""
	if s.Explain != nil {
		explained = s.Explain(err)
	}
	switch {
	case ctx.Err() != nil:
		msg = ReasonInterrupted
	case explained != "":
		msg = explained
	case errors.Is(err, modeladapter.ErrCircuitOpen):
		msg = "このモデルのベンダーで失敗が続いているため、呼び出しを止めています。しばらく待ってから実行し直してください"
	case errors.Is(err, modeladapter.ErrNoAPIKey):
		msg = "API キーが登録されていません"
	case errors.Is(err, modeladapter.ErrTimeout):
		msg = "ベンダーの応答が時間内に返りませんでした（ベンダー側では生成が続き、課金される場合があります）: " + err.Error()
	case errors.Is(err, direct.ErrInputsUnsupported), errors.Is(err, direct.ErrInputsConflict), errors.Is(err, direct.ErrResolution):
		msg = "このモデルはその入力・解像度を受け付けません: " + err.Error()
	case errors.Is(err, chatgpt.ErrNoImageModel): // before ErrRejected: the vendor's 400 is the symptom, not what to tell
		msg = chatgpt.ImageModelUnavailableMessage
	case errors.Is(err, modeladapter.ErrRejected):
		msg = "ベンダーがリクエストを拒否しました: " + err.Error()
	default:
		msg = err.Error()
	}
	return textutil.TruncateRunes(msg, maxErrorRunes)
}
