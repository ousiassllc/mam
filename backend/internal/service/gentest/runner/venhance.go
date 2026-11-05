package runner

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/agent/cloudenhance"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui/live"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/enhancevideo"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/venhance"
)

// refusal is a failure the app words itself (#1010): cause says why and what to do, in Japanese, under the code of its kind; err is the detail.
// Without it the report would map a plain error to "unknown" ("原因がわかりませんでした").
func refusal(code apperr.Code, cause string, err error) error {
	return apperr.New(code).WithCause(cause).Wrap(err)
}

// interruptedAtTopaz is the failure of a cloud job that mam stopped waiting for after Topaz accepted it: the job goes on at Topaz (the cancel call is
// not implemented), so the user is told the request id to look it up by.
func interruptedAtTopaz(requestID string, err error) error {
	return apperr.New(apperr.CodeTopazInterrupted).With("request_id", requestID).WithDetail("Topaz requestId " + requestID).Wrap(err)
}

// isInterruptedAtTopaz reports whether err is the failure of interruptedAtTopaz: it is settled with its own sentence, not as an interrupted result.
func isInterruptedAtTopaz(err error) bool {
	e, ok := apperr.As(err)
	return ok && e.Code == apperr.CodeTopazInterrupted
}

// enhanceVideo makes one result of "make a video larger / smoother": the source is the stored file of the result it started from (a batch of
// an enhancement has no input image), and the video is made by the local workflow of the model or by the paid cloud. A failed cloud run is
// failed as it is: it is never submitted again (the credits were reserved when the provider accepted it).
func (br *batchRun) enhanceVideo(ctx context.Context, r gentestrepo.Result, p record.ResultParams) (file, mime string, size int64, err error) {
	ve := p.VideoEnhance
	src, err := br.s.Repo.GetResult(ctx, ve.SourceResultID)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && src.FilePath == "") {
		return "", "", 0, refusal(apperr.CodeNotFound, "元の動画の結果が削除されたため、処理できません。元の動画をもう一度生成してから、やり直してください。", nil)
	}
	if err != nil {
		return "", "", 0, err
	}
	srcPath := filepath.Join(br.s.Dir, filepath.FromSlash(src.FilePath))
	file = r.ID + ".mp4"
	if ve.Provider == string(cloudenhance.ProviderTopaz) {
		size, err = br.enhanceInCloud(ctx, ve, srcPath, filepath.Join(br.dir, file))
		return file, "video/mp4", size, err
	}
	plan, err := venhance.Resolve(sourceOf(ve), paramsOf(ve), true, math.MaxInt)
	if err != nil {
		return "", "", 0, err
	}
	data, err := os.ReadFile(srcPath) //nolint:gosec // the path is the stored file of a result below the gentest directory
	if err != nil {
		return "", "", 0, refusal(apperr.CodeNotFound, "元の動画のファイルを読み込めませんでした（削除・移動されたか、読み取れません）。元の動画の結果を確かめ、必要なら生成し直してください。", err)
	}
	out, err := enhancevideo.Run(ctx, br.s.Adapter, domain.ModelName(r.Model), enhancevideo.Request{
		Source: modeladapter.Input{Data: data, MimeType: src.MimeType}, Scale: plan.Scale, Multiplier: plan.Multiplier, OutputFPS: plan.FPS,
	})
	if err != nil {
		return "", "", 0, err
	}
	file = r.ID + extensionFor(out.MimeType)
	size = out.File.Size()
	if err := out.File.StoreAs(filepath.Join(br.dir, file), 0o600); err != nil {
		return "", "", 0, fmt.Errorf("store the video: %w", err)
	}
	return file, out.MimeType, size, nil
}

// enhanceInCloud runs the quoted job of the cloud: the source is streamed from its file, the result is written beside its place and moved there when it
// is whole. The progress of the provider is shown as the progress of the job.
func (br *batchRun) enhanceInCloud(ctx context.Context, ve *record.VideoEnhance, srcPath, dest string) (int64, error) {
	if br.s.Cloud == nil {
		return 0, refusal(apperr.CodeUnavailable, "この mam ではクラウドの動画処理（Topaz）が使えません。ローカルのモデル（SeedVR2・RIFE）で実行してください。", nil)
	}
	if ve.Topaz == nil || ve.Topaz.RequestID == "" {
		return 0, refusal(apperr.CodeInvalidInput, "見積りの情報がこの結果に残っていないため、Topaz には送れません。もう一度、見積りを取ってから実行してください。", nil)
	}
	part := dest + ".part"
	defer func() { _ = os.Remove(part) }()
	job := live.Default.Start("topaz", ve.Topaz.RequestID)
	job.Own(live.OwnerOf(ctx))
	defer job.End()
	_, err := br.s.Cloud.Run(ctx, cloudenhance.RunInput{RequestID: ve.Topaz.RequestID, SourcePath: srcPath, DestPath: part}, func(p cloudenhance.Progress) {
		pct := int(max(0, min(100, p.Percent)))
		job.Step("topaz", "Topaz: "+p.Status, pct, 100, pct)
	})
	if err != nil {
		if ctx.Err() != nil && errors.Is(err, modeladapter.ErrAfterSubmit) {
			return 0, interruptedAtTopaz(ve.Topaz.RequestID, err)
		}
		return 0, err
	}
	if err := os.Rename(part, dest); err != nil {
		return 0, fmt.Errorf("store the video: %w", err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		return 0, fmt.Errorf("store the video: %w", err)
	}
	return info.Size(), nil
}

// sourceOf is the facts of the source a result remembers (the size and the frame rate are what the values are worked out from).
func sourceOf(ve *record.VideoEnhance) cloudenhance.Source {
	return cloudenhance.Source{Width: ve.SourceWidth, Height: ve.SourceHeight, FPS: ve.SourceFPS, FrameCount: ve.SourceFrames}
}

// paramsOf is what was asked of the output.
func paramsOf(ve *record.VideoEnhance) cloudenhance.Params {
	return cloudenhance.Params{
		Op: cloudenhance.Op(ve.Op), Scale: ve.Scale, TargetShortSide: ve.TargetShortSide, Multiplier: ve.Multiplier, TargetFPS: ve.TargetFPS,
	}
}
