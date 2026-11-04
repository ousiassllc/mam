package face_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/facesim"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/face"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
)

var modelsOff = &facesim.Off{Reason: facesim.ReasonModelsMissing, ModelDir: "/data/models", Missing: []string{facesim.DetectorFile, facesim.RecognizerFile}}

// ran runs one result of the editing model with the given roles while the face similarity is off.
func ran(t *testing.T, off *facesim.Off, roles ...string) (*gentesttest.Rig, record.ResultDetail) {
	t.Helper()
	r := gentesttest.New(t, gentesttest.WithReferenceModels(""), gentesttest.WithFaceOff(face.OffRecord(off)))
	req := imageReq(string(gentesttest.EditModel))
	req.References = stage(t, r, roles...)
	ref, err := r.Confirmed(req)
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	require.Equal(t, gentest.StateSucceeded, st.State, "%+v", st.Results)
	d, err := r.Svc.Result(context.Background(), st.Results[0].ID)
	require.NoError(t, err)
	return r, d
}

// The report of #1026: without the face models a result made from a face reference had no face_similarity at all. It now says it is skipped and why, with the
// fix: install them from the model manager (and where to put them by hand), as the common error words (apperr).
func TestAResultWithAFaceReferenceSaysTheFaceModelsAreMissing(t *testing.T) {
	_, d := ran(t, modelsOff, "face")

	require.NotNil(t, d.FaceSimilarity, "the field is there while the face similarity is off")
	fs := *d.FaceSimilarity
	assert.Equal(t, record.SkipModelsMissing, fs.Skipped)
	assert.Equal(t, record.MetricSFace, fs.Metric)
	assert.Zero(t, fs.Cosine)
	assert.False(t, fs.SamePerson)
	assert.Equal(t, []string{facesim.DetectorFile, facesim.RecognizerFile}, fs.MissingFiles)
	assert.Equal(t, "/data/models", fs.ModelDir)
	assert.Contains(t, fs.Reason, facesim.DetectorFile)
	assert.Contains(t, fs.Fix, "/data/models", "the fix names the folder")
	assert.Contains(t, fs.Fix, "顔チェック用モデル", "the fix points to the model manager's section")
	assert.Contains(t, fs.Fix, "/model-manager")
	assert.Contains(t, fs.Fix, "--face-model-dir")
	assert.NotContains(t, fs.Fix, "再起動してください", "no restart is needed any more")
}

// The ONNX Runtime library is another reason, with its own words.
func TestAResultSaysTheONNXRuntimeIsUnavailable(t *testing.T) {
	_, d := ran(t, &facesim.Off{Reason: facesim.ReasonRuntimeMissing, ModelDir: "/data/models", Detail: "onnxruntime.dll: not found"}, "face")

	require.NotNil(t, d.FaceSimilarity)
	assert.Equal(t, record.SkipRuntimeMissing, d.FaceSimilarity.Skipped)
	assert.Contains(t, d.FaceSimilarity.Fix, "--onnxruntime-lib")
	assert.Contains(t, d.FaceSimilarity.Fix, "導入し直しても直りません", "installing the models is not the fix for a missing library")
	assert.Empty(t, d.FaceSimilarity.MissingFiles)
}

// Without a reference of the role "face" there is nothing to measure whether it is on or off: no field, as before (not the "off" record).
func TestAResultWithoutAFaceReferenceHasNoFaceSimilarityWhileItIsOff(t *testing.T) {
	_, d := ran(t, modelsOff, "reference", "style")
	assert.Nil(t, d.FaceSimilarity)
}

func TestOffRecordIsNilWhileTheEngineIsOn(t *testing.T) {
	assert.Nil(t, face.OffRecord(nil))
}

// The confirmation says it before the batch runs, and only for a request that has a face reference.
func TestTheEstimateWarnsThatTheFaceSimilarityIsOff(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithReferenceModels(""), gentesttest.WithFaceOff(face.OffRecord(modelsOff)))
	req := imageReq(string(gentesttest.EditModel))
	req.References = stage(t, r, "face")
	est, err := r.Svc.Estimate(context.Background(), req)
	require.NoError(t, err)
	var got []gentest.Warning
	for _, w := range est.Warnings {
		if w.Code == record.WarnFaceSimilarityOff {
			got = append(got, w)
		}
	}
	require.Len(t, got, 1)
	assert.Contains(t, got[0].Message, facesim.RecognizerFile)

	req.References = stage(t, r, "style")
	est, err = r.Svc.Estimate(context.Background(), req)
	require.NoError(t, err)
	for _, w := range est.Warnings {
		assert.NotEqual(t, record.WarnFaceSimilarityOff, w.Code, "no face reference: nothing to say")
	}
}

// The status the MCP tool list_generation_models returns.
func TestTheStatusSaysWhetherTheFaceSimilarityIsMeasured(t *testing.T) {
	off := record.StatusOf(false, face.OffRecord(modelsOff))
	assert.False(t, off.Enabled)
	require.NotNil(t, off.Off)
	assert.Equal(t, record.SkipModelsMissing, off.Off.Skipped)
	assert.Equal(t, record.MetricSFace, off.Off.Metric)

	on := record.StatusOf(true, nil)
	assert.True(t, on.Enabled)
	assert.Nil(t, on.Off)
}

func TestFaceOffNoteOnlyForAFaceReferenceWhileItIsOff(t *testing.T) {
	off := face.OffRecord(modelsOff)
	_, _, ok := record.FaceOffNote(false, off, []string{"reference", "style"})
	assert.False(t, ok, "no face reference")
	_, _, ok = record.FaceOffNote(true, off, []string{"face"})
	assert.False(t, ok, "the similarity is measured")
	_, _, ok = record.FaceOffNote(false, nil, []string{"face"})
	assert.False(t, ok, "a service without the face similarity says nothing")
}
