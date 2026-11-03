package runner

import (
	"context"
	"encoding/json"

	"github.com/ousiassllc/mam/backend/internal/facesim"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
)

// FaceScoreFunc measures how much the face of generated is the face of reference (two image files): facesim.Compare over the face models.
// The application gives none when the models are not installed, and the results then carry no number.
type FaceScoreFunc func(ctx context.Context, reference, generated []byte) (facesim.Comparison, error)

// scoreFace measures the face of a result just made against the reference the batch gave it with the role "face" and writes the number
// into the result's params (record.FaceSimilarity, #998): the same SFace metric for every model, strength and role, so that the effect of
// a setting is a number and not an impression. It does nothing without FaceScore or without such a reference (the picture of the
// reference is the file as uploaded, not its crop: the detector takes the largest face of it either way). A measurement that fails
// is recorded as skipped and never fails the result: the picture is the user's whatever the number is.
func (br *batchRun) scoreFace(ctx context.Context, r gentestrepo.Result, made []byte) {
	s := br.s
	if s.FaceScore == nil || ctx.Err() != nil {
		return
	}
	var reference []byte
	for _, in := range br.inputsOf(r) {
		if in.role == gentestrepo.RoleFace {
			reference = in.in.Data
			break
		}
	}
	if reference == nil {
		return
	}
	fs := record.FaceSimilarity{Metric: record.MetricSFace, Threshold: facesim.SameFaceThreshold}
	cmp, err := s.FaceScore(ctx, reference, made)
	switch {
	case err != nil:
		if ctx.Err() != nil {
			return
		}
		s.Logger.Warn("measure the face similarity of a result failed", "result_id", r.ID, "err", err)
		fs.Skipped = record.SkipError
	case cmp.Missing == facesim.FirstHasNoFace:
		fs.Skipped = record.SkipReferenceNoFace
	case cmp.Missing == facesim.SecondHasNoFace:
		fs.Skipped = record.SkipResultNoFace
	default:
		fs.Cosine, fs.SamePerson = cmp.Cosine, cmp.Cosine >= facesim.SameFaceThreshold
	}
	// The other keys of the params stay as they are (the prompt match keeps its own in them).
	params := map[string]json.RawMessage{}
	_ = json.Unmarshal(r.Params, &params)
	params["face_similarity"], _ = json.Marshal(fs)
	raw, _ := json.Marshal(params)
	if err := s.Repo.SetResultParams(context.WithoutCancel(ctx), r.ID, raw); err != nil {
		s.Logger.Error("record the face similarity of a result failed", "result_id", r.ID, "err", err)
	}
}
