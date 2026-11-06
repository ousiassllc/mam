package record_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/facesim"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
)

func faceResult(model string, f *record.FaceSimilarity) record.ResultView {
	return record.ResultView{Model: model, ModelLabel: model + " label", FaceSimilarity: f}
}

func measured(cosine float64) *record.FaceSimilarity {
	return &record.FaceSimilarity{Metric: record.MetricSFace, Cosine: cosine, Threshold: facesim.SameFaceThreshold, SamePerson: cosine >= facesim.SameFaceThreshold}
}

// The similarities of a model's results are read together: the mean, the spread and how many pass the threshold, with the results that got no number counted apart.
func TestSummarizeFacesReadsTheSimilaritiesOfEachModelTogether(t *testing.T) {
	got := record.SummarizeFaces([]record.ResultView{
		faceResult("a", measured(0.6)),
		faceResult("b", measured(0.2)),
		faceResult("a", measured(0.2)),
		faceResult("a", &record.FaceSimilarity{Metric: record.MetricSFace, Skipped: record.SkipResultNoFace}),
		faceResult("b", measured(0.4)),
		faceResult("a", measured(0.4)),
	})

	require.Len(t, got, 2)
	assert.Equal(t, "a", got[0].Model, "the order the models first appear in")
	assert.Equal(t, "a label", got[0].ModelLabel)
	assert.Equal(t, 3, got[0].Measured)
	assert.Equal(t, 1, got[0].Skipped, "a result with a face reference and no number is counted apart, and is not in the figures")
	assert.InDelta(t, 0.4, got[0].MeanCosine, 1e-9)
	assert.InDelta(t, 0.2, got[0].MinCosine, 1e-9)
	assert.InDelta(t, 0.6, got[0].MaxCosine, 1e-9)
	assert.Equal(t, 2, got[0].SamePerson, "0.6 and 0.4 are at or above 0.363, 0.2 is not")
	assert.InDelta(t, 2.0/3, got[0].SamePersonRate, 1e-9)
	assert.InDelta(t, facesim.SameFaceThreshold, got[0].Threshold, 1e-9)

	assert.Equal(t, "b", got[1].Model)
	assert.Equal(t, 2, got[1].Measured)
	assert.InDelta(t, 0.3, got[1].MeanCosine, 1e-9)
	assert.Equal(t, 1, got[1].SamePerson)
}

// Nothing to say for a batch without a face reference; a model whose results all went without a number has the counts and no figures.
func TestSummarizeFacesIsEmptyWithoutAFaceReferenceAndHasNoFiguresWithoutANumber(t *testing.T) {
	assert.Nil(t, record.SummarizeFaces(nil))
	assert.Nil(t, record.SummarizeFaces([]record.ResultView{faceResult("a", nil)}), "no face_similarity: the batch had no reference of the role face")

	got := record.SummarizeFaces([]record.ResultView{
		faceResult("a", &record.FaceSimilarity{Metric: record.MetricSFace, Skipped: record.SkipModelsMissing}),
		faceResult("a", &record.FaceSimilarity{Metric: record.MetricSFace, Skipped: record.SkipError}),
	})
	require.Len(t, got, 1)
	assert.Equal(t, 0, got[0].Measured)
	assert.Equal(t, 2, got[0].Skipped)
	assert.Zero(t, got[0].MeanCosine)
	assert.Zero(t, got[0].MinCosine, "no infinity leaks out of a model with nothing measured")
	assert.Zero(t, got[0].MaxCosine)
	assert.Zero(t, got[0].SamePersonRate)
}
