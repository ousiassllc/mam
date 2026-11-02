package match

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/matchinfo"
)

// inputTypes are the stored extensions of the image types a reference can have (gentest.StageReference makes them).
var inputTypes = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp"}

// payload is the payload of the job of the generation test: the batch it runs.
type payload struct {
	BatchID string `json:"batch_id"`
}

// Run starts a match as a job, if the confirmation is the one of what the request costs now: the service prices the request again
// and refuses (gterr.ConflictError) when the estimate id or the total differs from the confirmed ones or the cost is over the limit.
// Nothing is generated before that. The rounds are planned now (queued, without a prompt): the job writes each prompt when its round
// starts and removes the rounds it did not need.
func (s *Service) Run(ctx context.Context, r Request, c gentest.Confirmation) (gentest.RunRef, error) {
	if s.g.Jobs == nil || s.g.Repo == nil {
		return gentest.RunRef{}, gterr.ErrUnavailable
	}
	est, p, err := s.estimate(ctx, r)
	if err != nil {
		return gentest.RunRef{}, err
	}
	if err := check(c, est); err != nil {
		return gentest.RunRef{}, err
	}
	batchID := p.req.ContinueBatchID
	if batchID == "" {
		if batchID, err = s.create(ctx, p, est); err != nil {
			return gentest.RunRef{}, err
		}
	} else if err := s.g.Repo.ContinueBatch(ctx, batchID, s.rounds(batchID, p, s.unit(est)), est.TotalUSD); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return gentest.RunRef{}, gterr.Conflict("このマッチはすでに続きが始まっています")
		}
		return gentest.RunRef{}, err
	}
	job, err := s.g.Jobs.Enqueue(ctx, domain.JobGentest, payload{BatchID: batchID})
	if err == nil {
		err = s.g.Repo.SetBatchJob(ctx, batchID, job.ID)
	}
	if err != nil {
		if p.req.ContinueBatchID == "" {
			_ = s.g.Repo.DeleteBatch(context.WithoutCancel(ctx), batchID)
			_ = os.RemoveAll(filepath.Join(s.g.DataDir, gentest.DirName, batchID))
		} else {
			_, _ = s.g.Repo.DeleteQueued(context.WithoutCancel(ctx), batchID)
			_ = s.g.Repo.FinishBatch(context.WithoutCancel(ctx), batchID, s.g.Now().UTC())
		}
		return gentest.RunRef{}, err
	}
	return gentest.RunRef{BatchID: batchID, JobID: job.ID, TotalItems: p.rounds}, nil
}

func (s *Service) unit(e Estimate) *float64 { return e.Breakdown[0].UnitUSD }

// create writes a new match: the batch, the reference image copied into the directory of the batch and the planned rounds.
func (s *Service) create(ctx context.Context, p plan, est Estimate) (string, error) {
	ref := p.req.Reference
	src, err := s.g.ResolveInput(ctx, ref)
	if err != nil {
		return "", err
	}
	id := domain.NewID()
	dir := filepath.Join(s.g.DataDir, gentest.DirName, id)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	fail := func(err error) (string, error) {
		_ = os.RemoveAll(dir)
		return "", err
	}
	inID := domain.NewID()
	name := "in-" + inID + inputTypes[src.Mime]
	if err := fsutil.CopyFile(src.Path, filepath.Join(dir, name)); err != nil {
		return fail(err)
	}
	bp := batchParams{AspectRatio: aspectOf(p.req), Count: 1, Match: &struct {
		Target int `json:"target_score"`
	}{p.req.TargetScore}}
	params, _ := json.Marshal(bp)
	estimate, _ := json.Marshal(est)
	nb := gentestrepo.NewBatch{
		Batch:  gentestrepo.Batch{ID: id, Media: gentestrepo.MediaImage, Params: params, EstimatedCost: est.TotalUSD, Estimate: estimate, CreatedAt: s.g.Now().UTC()},
		Inputs: []gentestrepo.Input{{ID: inID, BatchID: id, Role: gentestrepo.RoleReference, FilePath: path.Join(id, name), MimeType: src.Mime, SizeBytes: src.Size, Origin: src.Origin}},
	}
	nb.Results = s.rounds(id, p, s.unit(est))
	if err := s.g.Repo.CreateBatch(ctx, nb); err != nil {
		return fail(err)
	}
	if ref.Source == gentest.FromStaged {
		_ = os.Remove(src.Path) // the staged upload is consumed
	}
	return id, nil
}

// rounds plans the rounds of a run as queued results without a prompt. The first one carries the words of the user (the job reads them
// from there); every one carries the target.
func (s *Service) rounds(batchID string, p plan, unit *float64) []gentestrepo.Result {
	cfg := s.g.Adapter.Config()
	now := s.g.Now().UTC()
	basis := gentestrepo.CostUnknown
	switch {
	case unit == nil:
	case *unit == 0:
		basis = gentestrepo.CostFree
	default:
		basis = gentestrepo.CostPriced
	}
	out := make([]gentestrepo.Result, p.rounds)
	for i := range out {
		info := matchinfo.Round{Round: p.first + i + 1, Target: p.req.TargetScore}
		if i == 0 {
			info.Instruction = p.req.Instruction
		}
		raw, _ := json.Marshal(resultParams{AspectRatio: aspectOf(p.req), Match: &info})
		out[i] = gentestrepo.Result{
			ID: domain.NewID(), BatchID: batchID, Position: p.first + i, Media: gentestrepo.MediaImage, Model: p.model.ID,
			APIModel: cfg.ModelSettings(domain.ModelName(p.model.ID)).Model, NegativeMode: gentestrepo.NegativeNone, Params: raw,
			CostBasis: basis, EstimatedCost: unit, CreatedAt: now,
		}
		if p.model.SupportsSeed {
			seed := s.g.Seed()
			out[i].Seed, out[i].SeedSent = &seed, true
		}
	}
	return out
}
