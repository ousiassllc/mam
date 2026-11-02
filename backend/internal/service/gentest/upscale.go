package gentest

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg" // the size of the source is read from its header
	_ "image/png"
	"os"
	"path"
	"path/filepath"
	"slices"

	_ "golang.org/x/image/webp"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
)

// UpscaleScales are the scales "make it larger" takes: the upscale model's own factor is fitted to them with a resize.
var UpscaleScales = []int{2, 4}

// MaxUpscaleOutputPixels is the most pixels (width × height) a result of "make it larger" has: 8192 × 8192. A larger one would be a PNG
// near the limit of what the engine's answer is read at (localhttp.MaxFileBytes) and a picture most viewers cannot open. A workflow may
// set a lower limit of its own (the two-pass finish samples the whole picture: 4096 × 4096).
const MaxUpscaleOutputPixels = 64 << 20

// UpscaleRequest is "make it larger": the succeeded image result to enlarge, the scale (UpscaleScales) and the upscale workflow.
type UpscaleRequest struct {
	ResultID string
	Scale    int
	// Model is the upscale workflow (modelcatalog.Upscaler.ID). "": the first one that is installed and enlarges with the upscale model
	// alone (Mode "model").
	Model string
}

// UpscaleRef is a started enlargement: the batch and the job (RunRef) and the result it makes.
type UpscaleRef struct {
	RunRef
	ResultID string `json:"result_id" jsonschema:"大きくした結果の result_id（終わるまでは status が queued / running）"`
}

// Upscalers lists the upscale workflows the running setting has installed (an upscale model of the model manager that is not installed
// has none), with what each is and whether it can run now.
func (s *Service) Upscalers(_ context.Context) []modelcatalog.Upscaler { return s.catalog.Upscalers() }

// Upscale enlarges a succeeded image result by 2x or 4x as a batch of its own with one result (a file of its own: the source stays),
// run by the upscale workflow as a gentest job. The result remembers the source and the values (record.Upscale). An upscale workflow is
// a local model, so there is nothing to price or confirm: it is started at once. A two-pass workflow takes the prompt, the negative
// prompt and the seed of the source. The source may be a result of "make it larger" itself (it is enlarged again).
func (s *Service) Upscale(ctx context.Context, r UpscaleRequest) (UpscaleRef, error) {
	if s.Jobs == nil || s.Repo == nil {
		return UpscaleRef{}, gterr.ErrUnavailable
	}
	if !slices.Contains(UpscaleScales, r.Scale) {
		return UpscaleRef{}, gterr.Invalid("倍率は 2 か 4 にしてください")
	}
	srcResult, err := s.Repo.GetResult(ctx, r.ResultID)
	if err != nil {
		return UpscaleRef{}, gterr.NotFound(err)
	}
	src, err := s.ResolveInput(ctx, RefInput{Source: FromHistory, ID: r.ResultID})
	if err != nil {
		return UpscaleRef{}, err
	}
	up, err := s.pickUpscaler(r.Model)
	if err != nil {
		return UpscaleRef{}, err
	}
	info, err := s.upscaleOf(srcResult, src, up, r.Scale)
	if err != nil {
		return UpscaleRef{}, err
	}
	nb, err := s.newUpscaleBatch(srcResult, src, up, info)
	if err != nil {
		return UpscaleRef{}, err
	}
	if err := s.Repo.CreateBatch(ctx, nb); err != nil {
		_ = os.RemoveAll(filepath.Join(s.dir(), nb.Batch.ID))
		return UpscaleRef{}, err
	}
	job, err := s.Jobs.Enqueue(ctx, domain.JobGentest, record.Payload{BatchID: nb.Batch.ID})
	if err == nil {
		err = s.Repo.SetBatchJob(ctx, nb.Batch.ID, job.ID)
	}
	if err != nil {
		_ = s.Repo.DeleteBatch(context.WithoutCancel(ctx), nb.Batch.ID)
		_ = os.RemoveAll(filepath.Join(s.dir(), nb.Batch.ID))
		return UpscaleRef{}, err
	}
	return UpscaleRef{RunRef: RunRef{BatchID: nb.Batch.ID, JobID: job.ID, TotalItems: 1}, ResultID: nb.Results[0].ID}, nil
}

// pickUpscaler is the workflow named, or the first available one that needs no prompt; it must be installed and callable now.
func (s *Service) pickUpscaler(id string) (modelcatalog.Upscaler, error) {
	all := s.catalog.Upscalers()
	if id == "" {
		for _, u := range all {
			if u.Available && !u.TakesPrompt {
				return u, nil
			}
		}
		return modelcatalog.Upscaler{}, gterr.Invalid("アップスケール用のモデルが入っていません。モデルマネージャーでアップスケール用のモデルを入れてください")
	}
	i := slices.IndexFunc(all, func(u modelcatalog.Upscaler) bool { return u.ID == id })
	switch {
	case i < 0:
		return modelcatalog.Upscaler{}, gterr.Invalid("アップスケールのモデル %q は入っていません（upscale_models の一覧にあるものを選んでください）", id)
	case !all[i].Available:
		return modelcatalog.Upscaler{}, gterr.Invalid("アップスケールのモデル %q は今は使えません: %s", id, all[i].UnavailableMessage)
	}
	return all[i], nil
}

// upscaleOf is the record of the enlargement; it refuses an output that would have more pixels than the limit.
func (s *Service) upscaleOf(res gentestrepo.Result, src Source, up modelcatalog.Upscaler, scale int) (record.Upscale, error) {
	f, err := os.Open(src.Path)
	if err != nil {
		return record.Upscale{}, gterr.Invalid("元の画像のファイルが読めません")
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return record.Upscale{}, gterr.Invalid("元の画像の大きさを読めません: %v", err)
	}
	limit := MaxUpscaleOutputPixels
	if up.MaxOutputPixels > 0 {
		limit = min(limit, up.MaxOutputPixels)
	}
	if w, h := cfg.Width*scale, cfg.Height*scale; w*h > limit {
		return record.Upscale{}, gterr.Invalid("%d 倍にすると %d × %d（%d 万画素）になり、このモデルの上限 %d 万画素を超えます。倍率を下げるか、先に小さくしてください",
			scale, w, h, w*h/10000, limit/10000)
	}
	return record.Upscale{
		SourceResultID: res.ID, SourceModel: res.Model, Scale: scale, Model: up.ID, Mode: up.Mode, Native: up.Native,
		SourceWidth: cfg.Width, SourceHeight: cfg.Height, Width: cfg.Width * scale, Height: cfg.Height * scale,
	}, nil
}

// newUpscaleBatch copies the source into the batch's directory and plans the one result. The batch has no estimate (a local model has
// nothing to confirm) and no prompt of the prompt history.
func (s *Service) newUpscaleBatch(res gentestrepo.Result, src Source, up modelcatalog.Upscaler, info record.Upscale) (gentestrepo.NewBatch, error) {
	now := s.Now().UTC()
	id := domain.NewID()
	dir := filepath.Join(s.dir(), id)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return gentestrepo.NewBatch{}, err
	}
	fail := func(err error) (gentestrepo.NewBatch, error) {
		_ = os.RemoveAll(dir)
		return gentestrepo.NewBatch{}, err
	}
	var source record.ResultParams
	_ = json.Unmarshal(res.Params, &source)
	params, _ := json.Marshal(record.BatchParams{AspectRatio: source.AspectRatio, Count: 1})
	inputID := domain.NewID()
	name := "in-" + inputID + inputTypes[src.Mime]
	if err := fsutil.CopyFile(src.Path, filepath.Join(dir, name)); err != nil {
		return fail(fmt.Errorf("copy input image: %w", err))
	}
	zero := 0.0
	nb := gentestrepo.NewBatch{
		Batch: gentestrepo.Batch{ID: id, Media: gentestrepo.MediaImage, Params: params, CreatedAt: now},
		Inputs: []gentestrepo.Input{{
			ID: inputID, BatchID: id, Role: gentestrepo.RoleReference, FilePath: path.Join(id, name), MimeType: src.Mime, SizeBytes: src.Size,
			Origin: src.Origin, Position: 0,
		}},
	}
	out := record.ResultParams{AspectRatio: source.AspectRatio, Upscale: &info}
	result := gentestrepo.Result{
		ID: domain.NewID(), BatchID: id, Media: gentestrepo.MediaImage, Model: up.ID, APIModel: s.Adapter.Config().ModelSettings(domain.ModelName(up.ID)).Model,
		NegativeMode: gentestrepo.NegativeNone, UsedInputs: []string{inputID}, CostBasis: modelcatalog.BasisFree, EstimatedCost: &zero, CreatedAt: now,
	}
	if up.TakesPrompt {
		// The prompt the source was made with, as it was sent, and its seed when it had one: the finish draws the same picture again.
		result.Prompt, result.NegativePrompt, result.SentPrompt, result.NegativeMode = res.Prompt, res.NegativePrompt, res.SentPrompt, res.NegativeMode
		out.Translation = source.Translation
		seed := s.Seed()
		if res.Seed != nil {
			seed = *res.Seed
		}
		result.Seed, result.SeedSent = &seed, true
	}
	result.Params, _ = json.Marshal(out)
	nb.Results = []gentestrepo.Result{result}
	return nb, nil
}
