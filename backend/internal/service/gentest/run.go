package gentest

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/direct"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
)

// payload is the payload of a gentest job: the batch it runs.
type payload struct {
	BatchID string `json:"batch_id"`
}

// RunRef is a batch that was queued: the batch and the job that runs it.
type RunRef struct {
	BatchID string `json:"batch_id"`
	JobID   string `json:"job_id"`
	// TotalItems is the number of results the batch makes.
	TotalItems int `json:"total_items"`
}

// batchParams is gentest_batch.params.
type batchParams struct {
	AspectRatio     string  `json:"aspect_ratio"`
	Resolution      string  `json:"resolution,omitempty"`
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
	Count           int     `json:"count"`
}

// resultParams is gentest_result.params: what was asked of that model.
type resultParams struct {
	AspectRatio     string  `json:"aspect_ratio"`
	Resolution      string  `json:"resolution,omitempty"`
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
}

// randomSeed is a seed for FLUX.2 and Runway Gen-4.5: an unsigned 32-bit number (Runway takes 0 to 4294967295).
func randomSeed() int64 {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("gentest: no random seed: %v", err)) // crypto/rand does not fail on supported platforms
	}
	return int64(binary.BigEndian.Uint32(b[:]))
}

// Run starts a batch as a job, if the confirmation matches what the request costs now: the service prices the request again,
// and refuses (gterr.ConflictError) when the estimate id or the total differs from the confirmed ones or when the cost is over the
// limit. The limit of results is checked again here (gterr.InvalidError). Nothing is generated before that. The inputs are copied into
// the batch's directory; staged uploads are consumed.
func (s *Service) Run(ctx context.Context, r Request, c Confirmation) (RunRef, error) {
	if s.Jobs == nil || s.Repo == nil {
		return RunRef{}, gterr.ErrUnavailable
	}
	est, plans, err := s.estimate(ctx, r)
	if err != nil {
		return RunRef{}, err
	}
	if err := c.check(est); err != nil {
		return RunRef{}, err
	}
	r = est.Request
	sources := make([]source, len(r.References))
	for i, ref := range r.References {
		if sources[i], err = s.resolve(ctx, ref); err != nil {
			return RunRef{}, err
		}
	}
	nb, err := s.newBatch(r, est, plans, sources)
	if err != nil {
		return RunRef{}, err
	}
	if err := s.Repo.CreateBatch(ctx, nb); err != nil {
		_ = os.RemoveAll(filepath.Join(s.dir(), nb.Batch.ID))
		return RunRef{}, err
	}
	job, err := s.Jobs.Enqueue(ctx, domain.JobGentest, payload{BatchID: nb.Batch.ID})
	if err == nil {
		err = s.Repo.SetBatchJob(ctx, nb.Batch.ID, job.ID)
	}
	if err != nil {
		_ = s.Repo.DeleteBatch(context.WithoutCancel(ctx), nb.Batch.ID)
		_ = os.RemoveAll(filepath.Join(s.dir(), nb.Batch.ID))
		return RunRef{}, err
	}
	for _, ref := range r.References {
		if ref.Source == FromStaged {
			_ = os.Remove(filepath.Join(s.dir(), stagingDirName, ref.ID)) //nolint:gosec // a staged id has no separator (checked by StagedFile in resolve)
		}
	}
	return RunRef{BatchID: nb.Batch.ID, JobID: job.ID, TotalItems: est.TotalItems}, nil
}

// newBatch copies the inputs into the batch's directory and plans every result: the models in the order given, each with
// every prompt, each Count times, every result with its own seed when the model takes one.
func (s *Service) newBatch(r Request, est Estimate, plans []modelPlan, sources []source) (gentestrepo.NewBatch, error) {
	cfg := s.Adapter.Config()
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
	params, _ := json.Marshal(batchParams{AspectRatio: r.AspectRatio, Resolution: r.Resolution, DurationSeconds: r.DurationSeconds, Count: r.Count})
	estimate, _ := json.Marshal(est)
	nb := gentestrepo.NewBatch{Batch: gentestrepo.Batch{
		ID: id, Media: r.Media, Params: params, EstimatedCost: est.TotalUSD, Estimate: estimate, CreatedAt: now,
	}}
	inputIDs := make([]string, len(sources))
	for i, src := range sources {
		inputIDs[i] = domain.NewID()
		name := "in-" + inputIDs[i] + inputTypes[src.mime]
		if err := copyFile(src.path, filepath.Join(dir, name)); err != nil {
			return fail(fmt.Errorf("copy input image: %w", err))
		}
		nb.Inputs = append(nb.Inputs, gentestrepo.Input{
			ID: inputIDs[i], BatchID: id, Role: r.References[i].Role, FilePath: path.Join(id, name), MimeType: src.mime, SizeBytes: src.size,
			Origin: src.origin, Position: i,
		})
	}
	for _, p := range r.Prompts {
		nb.Prompts = append(nb.Prompts, gentestrepo.PromptKey{Prompt: p.Prompt, NegativePrompt: p.Negative})
	}
	pos := 0
	for _, mp := range plans {
		used := make([]string, len(mp.used))
		for i, idx := range mp.used {
			used[i] = inputIDs[idx]
		}
		rp, _ := json.Marshal(resultParams{AspectRatio: r.AspectRatio, Resolution: mp.resolution, DurationSeconds: mp.seconds})
		for _, p := range r.Prompts {
			for range r.Count {
				res := gentestrepo.Result{
					ID: domain.NewID(), BatchID: id, Position: pos, Media: r.Media, Model: mp.info.ID, APIModel: cfg.ModelSettings(domain.ModelName(mp.info.ID)).Model,
					Prompt: p.Prompt, NegativePrompt: p.Negative, SentPrompt: p.Prompt, NegativeMode: gentestrepo.NegativeNone, Params: rp,
					UsedInputs: used, CostBasis: mp.basis, EstimatedCost: mp.unit, CreatedAt: now,
				}
				if p.Negative != "" {
					res.SentPrompt, res.NegativeMode = foldFor(mp.info, p)
				}
				if mp.info.SupportsSeed {
					seed := s.Seed()
					res.Seed, res.SeedSent = &seed, true
				}
				nb.Results = append(nb.Results, res)
				pos++
			}
		}
	}
	return nb, nil
}

// foldFor is the prompt a model is sent and how its negative prompt reaches it: in the field of the model, or written into the
// prompt as an "Avoid: ..." sentence.
func foldFor(m modelcatalog.Info, p PromptInput) (sent, mode string) {
	if m.NegativeField {
		return p.Prompt, gentestrepo.NegativeField
	}
	return direct.Fold(p.Prompt, p.Negative), gentestrepo.NegativeFolded
}

func copyFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // src is a file resolve() found: staged, an asset of a Project or a history file
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	return fsutil.WriteAtomic(dst, 0o600, func(w io.Writer) error {
		_, err := io.Copy(w, in)
		return err
	})
}
