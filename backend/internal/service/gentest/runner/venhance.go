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
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/venhance"
)

// errNoCloud is why a result of the cloud cannot run when the service has no cloud.
var errNoCloud = errors.New("クラウドの動画処理（Topaz）はこのアプリでは使えません")

// enhanceVideo makes one result of "make a video larger / smoother": the source is the stored file of the result it started from (a batch of
// an enhancement has no input image), and the video is made by the local workflow of the model or by the paid cloud. A failed cloud run is
// failed as it is: it is never submitted again (the credits were reserved when the provider accepted it).
func (br *batchRun) enhanceVideo(ctx context.Context, r gentestrepo.Result, p record.ResultParams) (file, mime string, size int64, err error) {
	ve := p.VideoEnhance
	src, err := br.s.Repo.GetResult(ctx, ve.SourceResultID)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && src.FilePath == "") {
		return "", "", 0, errors.New("元の動画の結果が削除されたため、処理できません")
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
		return "", "", 0, fmt.Errorf("read the source video: %w", err)
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
		return 0, errNoCloud
	}
	if ve.Topaz == nil || ve.Topaz.RequestID == "" {
		return 0, errors.New("見積りの情報がないため、Topaz には送れません。もう一度、見積りを取って実行してください")
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
