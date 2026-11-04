package videoenhance

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/cloudenhance"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/venhance"
)

// Ref is a started enhancement: the batch and the job (gentest.RunRef) and the result it makes.
type Ref struct {
	gentest.RunRef
	ResultID string `json:"result_id" jsonschema:"作った結果の result_id（終わるまでは status が queued / running）"`
}

// creditsEpsilon is how far the credits the user confirmed may be from the quote: the same number written back.
const creditsEpsilon = 1e-6

// Enhance makes a succeeded video result larger or smoother as a batch of its own with one result (a file of its own: the source stays), run as a
// gentest job. A local model is free and starts at once. A cloud model starts only with the estimate (EstimateID) it was quoted by and the credits the
// user confirmed (ConfirmedCredits equal to the quote): a missing or changed one is refused, and the quote is used up (the same one is never run twice,
// so the credits are never reserved twice). The result remembers the source and the values (record.VideoEnhance). The source may be a result of an
// enhancement itself.
func Enhance(ctx context.Context, g *gentest.Service, r Request) (Ref, error) {
	if g.Jobs == nil || g.Repo == nil {
		return Ref{}, gterr.ErrUnavailable
	}
	t, err := prepare(ctx, g, r)
	if err != nil {
		return Ref{}, err
	}
	var topaz *record.TopazRun
	if t.model.Paid {
		var made cloudenhance.CloudEstimate
		if topaz, made, err = confirm(g, r, t); err != nil {
			return Ref{}, err
		}
		// The output the quote promised (the sizes of Topaz are multiples of 4) is what the result remembers, not the local plan's.
		if made.Width > 0 && made.Height > 0 && made.FPS > 0 {
			t.plan.Width, t.plan.Height, t.plan.FPS = made.Width, made.Height, made.FPS
		}
	}
	nb, err := newBatch(g, r, t, topaz)
	if err != nil {
		return Ref{}, err
	}
	if err := g.Repo.CreateBatch(ctx, nb); err != nil {
		_ = os.RemoveAll(filepath.Join(g.DataDir, gentest.DirName, nb.Batch.ID))
		return Ref{}, err
	}
	job, err := g.Jobs.Enqueue(ctx, domain.JobGentest, record.Payload{BatchID: nb.Batch.ID})
	if err == nil {
		err = g.Repo.SetBatchJob(ctx, nb.Batch.ID, job.ID)
	}
	if err != nil {
		_ = g.Repo.DeleteBatch(context.WithoutCancel(ctx), nb.Batch.ID)
		_ = os.RemoveAll(filepath.Join(g.DataDir, gentest.DirName, nb.Batch.ID))
		return Ref{}, err
	}
	return Ref{RunRef: gentest.RunRef{BatchID: nb.Batch.ID, JobID: job.ID, TotalItems: 1}, ResultID: nb.Results[0].ID}, nil
}

// confirm is the gate of a cloud model: the quote of EstimateID, for exactly this source, model and values, and the credits the user confirmed. It
// takes the quote (it runs once) and returns what the result remembers of it, with the quote itself.
func confirm(g *gentest.Service, r Request, t target) (*record.TopazRun, cloudenhance.CloudEstimate, error) {
	if r.EstimateID == "" {
		return nil, cloudenhance.CloudEstimate{}, gterr.Invalid("Topaz は有料のクラウドです。estimate_generation_video_enhance で見積りを取り、利用者に確認してから、estimate_id と confirmed_credits を渡してください")
	}
	q, ok := g.VideoEstimates.Get(r.EstimateID)
	if !ok {
		return nil, cloudenhance.CloudEstimate{}, errExpired()
	}
	if q.ResultID != r.ResultID || q.Model != t.model.ID || q.Plan.Params != t.plan.Params || q.Plan.Width != t.plan.Width || q.Plan.Height != t.plan.Height || q.Plan.FPS != t.plan.FPS {
		return nil, cloudenhance.CloudEstimate{}, gterr.Conflict("見積りのときと条件（元の動画・モデル・倍率や fps）が違います。もう一度見積りを取って、その額を確認してください")
	}
	// Written so that NaN, Inf and a negative number fail: every comparison with NaN is false, so the check asks for what is allowed, not for what is not.
	if d := math.Abs(r.ConfirmedCredits - q.Estimate.Credits); !(d <= creditsEpsilon) || r.ConfirmedCredits < 0 {
		return nil, cloudenhance.CloudEstimate{}, gterr.Conflict("確認したクレジット（%g）が見積り（%g）と違います。見積りの credits をそのまま confirmed_credits に渡してください", r.ConfirmedCredits, q.Estimate.Credits)
	}
	if _, ok := g.VideoEstimates.Take(r.EstimateID); !ok { // another call ran it between
		return nil, cloudenhance.CloudEstimate{}, errExpired()
	}
	return &record.TopazRun{RequestID: q.Estimate.RequestID, EstimatedCredits: q.Estimate.Credits, Attribution: venhance.TopazAttribution}, q.Estimate, nil
}

// newBatch plans the batch and its one result. The batch has no input image (the runner reads the file of the source result), no estimate in USD and
// no prompt of the prompt history. The cost is free for a local model and unknown for the cloud (its credits are not money: they are in the record).
func newBatch(g *gentest.Service, r Request, t target, topaz *record.TopazRun) (gentestrepo.NewBatch, error) {
	now := g.Now().UTC()
	id := domain.NewID()
	if err := os.MkdirAll(filepath.Join(g.DataDir, gentest.DirName, id), 0o750); err != nil {
		return gentestrepo.NewBatch{}, err
	}
	var source record.ResultParams
	_ = json.Unmarshal(t.res.Params, &source)
	params, _ := json.Marshal(record.BatchParams{AspectRatio: source.AspectRatio, DurationSeconds: source.DurationSeconds, Count: 1})
	ve := record.VideoEnhance{
		SourceResultID: t.res.ID, SourceModel: t.res.Model, Op: string(r.Op), Provider: t.model.Provider, Model: t.model.ID,
		Scale: r.Scale, TargetShortSide: r.TargetShortSide, Multiplier: r.Multiplier, TargetFPS: r.TargetFPS,
		SourceWidth: t.facts.Width, SourceHeight: t.facts.Height, SourceFPS: t.facts.FPS, SourceFrames: t.facts.FrameCount,
		Width: t.plan.Width, Height: t.plan.Height, FPS: t.plan.FPS, Topaz: topaz,
	}
	result := gentestrepo.Result{
		ID: domain.NewID(), BatchID: id, Media: gentestrepo.MediaVideo, Model: t.model.ID, APIModel: apiModel(g, t.model),
		NegativeMode: gentestrepo.NegativeNone, CostBasis: modelcatalog.BasisUnknown, CreatedAt: now,
	}
	if !t.model.Paid {
		zero := 0.0
		result.CostBasis, result.EstimatedCost = modelcatalog.BasisFree, &zero
	}
	result.Params, _ = json.Marshal(record.ResultParams{AspectRatio: source.AspectRatio, DurationSeconds: source.DurationSeconds, VideoEnhance: &ve})
	return gentestrepo.NewBatch{
		Batch:   gentestrepo.Batch{ID: id, Media: gentestrepo.MediaVideo, Params: params, CreatedAt: now},
		Results: []gentestrepo.Result{result},
	}, nil
}

// apiModel is the model's own name of itself: the name of a model of Topaz, the model name of a workflow's settings.
func apiModel(g *gentest.Service, m modelcatalog.VideoEnhancer) string {
	if isCloud(m.ID) {
		return strings.TrimPrefix(m.ID, CloudPrefix)
	}
	return g.Adapter.Config().ModelSettings(domain.ModelName(m.ID)).Model
}
