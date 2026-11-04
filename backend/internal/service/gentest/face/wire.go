package face

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/facefusion"
	"github.com/ousiassllc/mam/backend/internal/facesim"
	"github.com/ousiassllc/mam/backend/internal/service/facefusionsettings"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/runner"
)

// Crop is the cut of the face reference of the generation test to the face (#960): it uses the face detector of the Judge, so it works when
// the face models and the ONNX Runtime library are there, and is nil (the picture goes whole) when they are not.
func Crop(faces facesim.Engine) runner.FaceCropFunc {
	if faces == nil {
		return nil
	}
	return func(ctx context.Context, image []byte) ([]byte, bool, error) {
		return facesim.CropFace(ctx, faces, image)
	}
}

// Score is the measurement of the face similarity of a generation test result (#998): the SFace of the Judge's face engine. nil (no number)
// when the face models or the ONNX Runtime library are not there.
func Score(faces facesim.Engine) runner.FaceScoreFunc {
	if faces == nil {
		return nil
	}
	return func(ctx context.Context, reference, generated []byte) (facesim.Comparison, error) {
		return facesim.Compare(ctx, faces, reference, generated)
	}
}

// OffRecord is what a result with a face reference records while the face similarity is off (#1026): skipped models_missing (a model file is not in the
// model folder) or runtime_unavailable (the ONNX Runtime library), with the cause and the fix in words (apperr: which files to put where; mam does not
// fetch the models and the model manager's catalog does not list them). nil when off is nil (the engine is on).
func OffRecord(off *facesim.Off) *record.FaceSimilarity {
	if off == nil {
		return nil
	}
	e, skipped := apperr.New(apperr.CodeFaceRuntime).WithDetail(off.Detail), record.SkipRuntimeMissing
	if off.Reason == facesim.ReasonModelsMissing {
		e = apperr.New(apperr.CodeFaceModels).With("files", strings.Join(off.Missing, "{{list.separator}}")).With("dir", filepath.Clean(off.ModelDir))
		skipped = record.SkipModelsMissing
	}
	v := e.View(apperr.LangJa)
	return &record.FaceSimilarity{Metric: record.MetricSFace, Threshold: facesim.SameFaceThreshold, Skipped: skipped, Reason: v.Cause, Fix: v.Remedy, ModelDir: off.ModelDir, MissingFiles: off.Missing}
}

// Swapper is what the generation test needs of FaceFusion (Issue #995): the swap that starts `facefusion.py headless-run` of the install the
// settings find, and the check that says why there is none. The install is looked up on every swap, so a path saved in the settings applies
// to the next one.
func Swapper(ff *facefusionsettings.Service) (swap runner.FaceSwapFunc, check func() error) {
	check = func() error {
		_, err := ff.Look()
		return err
	}
	swap = func(ctx context.Context, in runner.FaceSwapInput) ([]byte, string, error) {
		install, err := ff.Look()
		if err != nil {
			return nil, "", apperr.New(apperr.CodeFaceFusionNone).Wrap(err)
		}
		faceExt, targetExt := extensions[in.Face.MimeType], extensions[in.Target.MimeType]
		if faceExt == "" || targetExt == "" {
			return nil, "", errors.New("FaceFusion に渡せる画像は PNG・JPEG・WebP です")
		}
		return install.Swap(ctx, facefusion.Request{
			Face: in.Face.Data, Target: in.Target.Data, FaceExt: faceExt, TargetExt: targetExt,
			Selection: facefusion.Selection{Swapper: in.Swapper, Detector: in.Detector, Enhancer: in.Enhancer},
		})
	}
	return swap, check
}
