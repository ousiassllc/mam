// Package upscale is "make it larger" of the generation test (Issue #975): the enlargement of a succeeded image result by 2x or 4x as a batch of its own.
// It is a package of its own (like videoenhance) so that package gentest stays under the line limit; it uses the service only through its exported
// methods and Deps. The upscale workflows installed are listed by gentest.Service.Upscalers.
package upscale

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
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
)

// Scales are the scales "make it larger" takes: the upscale model's own factor is fitted to them with a resize.
var Scales = []int{2, 4}

// MaxOutputPixels is the most pixels (width × height) a result of "make it larger" has: 8192 × 8192. A larger one would be a PNG
// near the limit of what the engine's answer is read at (localhttp.MaxFileBytes) and a picture most viewers cannot open. A workflow may
// set a lower limit of its own (the two-pass finish samples the whole picture: 4096 × 4096).
const MaxOutputPixels = 64 << 20

// Request is "make it larger": the succeeded image result to enlarge, the scale (Scales) and the upscale workflow.
type Request struct {
	ResultID string
	Scale    int
	// Model is the upscale workflow (modelcatalog.Upscaler.ID). "": the first one that is installed and enlarges with the upscale model
	// alone (Mode "model").
	Model string
}

// Ref is a started enlargement: the batch and the job (gentest.RunRef) and the result it makes.
type Ref struct {
	gentest.RunRef
	ResultID string `json:"result_id" jsonschema:"大きくした結果の result_id（終わるまでは status が queued / running）"`
}

// Run enlarges a succeeded image result by 2x or 4x as a batch of its own with one result (a file of its own: the source stays),
// run by the upscale workflow as a gentest job. The result remembers the source and the values (record.Upscale). An upscale workflow is
// a local model, so there is nothing to price or confirm: it is started at once. A two-pass workflow takes the prompt, the negative
// prompt and the seed of the source. The source may be a result of "make it larger" itself (it is enlarged again).
func Run(ctx context.Context, g *gentest.Service, r Request) (Ref, error) {
	if g.Jobs == nil || g.Repo == nil {
		return Ref{}, gterr.ErrUnavailable
	}
	if !slices.Contains(Scales, r.Scale) {
		return Ref{}, gterr.Invalid("倍率は 2 か 4 にしてください")
	}
	srcResult, err := g.Repo.GetResult(ctx, r.ResultID)
	if err != nil {
		return Ref{}, gterr.NotFound(err)
	}
	src, err := g.ResolveInput(ctx, gentest.RefInput{Source: gentest.FromHistory, ID: r.ResultID})
	if err != nil {
		return Ref{}, err
	}
	up, err := pick(g.Upscalers(ctx), r.Model)
	if err != nil {
		return Ref{}, err
	}
	info, err := upscaleOf(srcResult, src, up, r.Scale)
	if err != nil {
		return Ref{}, err
	}
	nb, err := newBatch(g, srcResult, src, up, info)
	if err != nil {
		return Ref{}, err
	}
	dir := filepath.Join(g.DataDir, gentest.DirName, nb.Batch.ID)
	if err := g.Repo.CreateBatch(ctx, nb); err != nil {
		_ = os.RemoveAll(dir)
		return Ref{}, err
	}
	job, err := g.Jobs.Enqueue(ctx, domain.JobGentest, record.Payload{BatchID: nb.Batch.ID})
	if err == nil {
		err = g.Repo.SetBatchJob(ctx, nb.Batch.ID, job.ID)
	}
	if err != nil {
		_ = g.Repo.DeleteBatch(context.WithoutCancel(ctx), nb.Batch.ID)
		_ = os.RemoveAll(dir)
		return Ref{}, err
	}
	return Ref{RunRef: gentest.RunRef{BatchID: nb.Batch.ID, JobID: job.ID, TotalItems: 1}, ResultID: nb.Results[0].ID}, nil
}

// pick is the workflow named, or the first available one that needs no prompt; it must be installed and callable now.
func pick(all []modelcatalog.Upscaler, id string) (modelcatalog.Upscaler, error) {
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
func upscaleOf(res gentestrepo.Result, src gentest.Source, up modelcatalog.Upscaler, scale int) (record.Upscale, error) {
	f, err := os.Open(src.Path)
	if err != nil {
		return record.Upscale{}, gterr.Invalid("元の画像のファイルが読めません")
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return record.Upscale{}, gterr.Invalid("元の画像の大きさを読めません: %v", err)
	}
	limit := MaxOutputPixels
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

// newBatch copies the source into the batch's directory and plans the one result. The batch has no estimate (a local model has
// nothing to confirm) and no prompt of the prompt history.
func newBatch(g *gentest.Service, res gentestrepo.Result, src gentest.Source, up modelcatalog.Upscaler, info record.Upscale) (gentestrepo.NewBatch, error) {
	now := g.Now().UTC()
	id := domain.NewID()
	dir := filepath.Join(g.DataDir, gentest.DirName, id)
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
	name := "in-" + inputID + gentest.InputExt(src.Mime)
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
		ID: domain.NewID(), BatchID: id, Media: gentestrepo.MediaImage, Model: up.ID, APIModel: g.Adapter.Config().ModelSettings(domain.ModelName(up.ID)).Model,
		NegativeMode: gentestrepo.NegativeNone, UsedInputs: []string{inputID}, CostBasis: modelcatalog.BasisFree, EstimatedCost: &zero, CreatedAt: now,
	}
	if up.TakesPrompt {
		// The prompt the source was made with, as it was sent, and its seed when it had one: the finish draws the same picture again.
		result.Prompt, result.NegativePrompt, result.SentPrompt, result.NegativeMode = res.Prompt, res.NegativePrompt, res.SentPrompt, res.NegativeMode
		out.Translation = source.Translation
		seed := g.Seed()
		if res.Seed != nil {
			seed = *res.Seed
		}
		result.Seed, result.SeedSent = &seed, true
	}
	result.Params, _ = json.Marshal(out)
	nb.Results = []gentestrepo.Result{result}
	return nb, nil
}
