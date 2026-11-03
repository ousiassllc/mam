package face_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/facesim"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
)

// scorer is a FaceScoreFunc that answers as scripted and records the pictures it was asked about.
type scorer struct {
	mu   sync.Mutex
	asks [][2][]byte
	cmp  facesim.Comparison
	err  error
}

func (s *scorer) score(_ context.Context, reference, generated []byte) (facesim.Comparison, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asks = append(s.asks, [2][]byte{reference, generated})
	return s.cmp, s.err
}

// scored runs one result of the editing model with the given roles and returns it with its detail.
func scored(t *testing.T, s *scorer, roles ...string) (*gentesttest.Rig, record.ResultDetail) {
	t.Helper()
	r := gentesttest.New(t, gentesttest.WithReferenceModels(""), gentesttest.WithFaceScore(s.score))
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

// A result made from a reference of the role "face" gets the SFace similarity of its face to the reference's, with the threshold to read it by.
func TestRunMeasuresTheFaceOfAResultAgainstTheFaceReference(t *testing.T) {
	s := &scorer{cmp: facesim.Comparison{Cosine: 0.52}}
	_, d := scored(t, s, "face", "style")

	require.NotNil(t, d.FaceSimilarity)
	assert.Equal(t, record.FaceSimilarity{Metric: "sface", Cosine: 0.52, Threshold: facesim.SameFaceThreshold, SamePerson: true}, *d.FaceSimilarity)
	require.Len(t, s.asks, 1, "one measurement, against the picture of the face role (not the style one)")
	assert.Equal(t, append(append([]byte(nil), gentesttest.PNG...), 0), s.asks[0][0], "the reference as uploaded, not its crop")
	assert.Equal(t, gentesttest.PNG, s.asks[0][1], "the picture the model made")
}

func TestRunSaysASimilarityBelowTheThresholdIsNotTheSamePerson(t *testing.T) {
	_, d := scored(t, &scorer{cmp: facesim.Comparison{Cosine: 0.1}}, "face")

	require.NotNil(t, d.FaceSimilarity)
	assert.False(t, d.FaceSimilarity.SamePerson)
	assert.InDelta(t, 0.1, d.FaceSimilarity.Cosine, 1e-9)
}

// What cannot be measured is recorded with the reason: a result without a number says why, and is a success all the same.
func TestRunRecordsWhyTheFaceCouldNotBeMeasured(t *testing.T) {
	for name, tc := range map[string]struct {
		s    *scorer
		want string
	}{
		"no face in the reference": {&scorer{cmp: facesim.Comparison{Missing: facesim.FirstHasNoFace}}, record.SkipReferenceNoFace},
		"no face in the result":    {&scorer{cmp: facesim.Comparison{Missing: facesim.SecondHasNoFace}}, record.SkipResultNoFace},
		"the engine fails":         {&scorer{err: errors.New("the model failed")}, record.SkipError},
	} {
		t.Run(name, func(t *testing.T) {
			_, d := scored(t, tc.s, "face")

			require.NotNil(t, d.FaceSimilarity)
			assert.Equal(t, tc.want, d.FaceSimilarity.Skipped)
			assert.Zero(t, d.FaceSimilarity.Cosine)
			assert.False(t, d.FaceSimilarity.SamePerson)
		})
	}
}

// Only the role "face" is a face to hold the result to: another reference, or none, is not measured; so is a service without the models.
func TestRunDoesNotMeasureWithoutAFaceReferenceOrWithoutTheModels(t *testing.T) {
	s := &scorer{}
	_, d := scored(t, s, "reference", "style")
	assert.Nil(t, d.FaceSimilarity)
	assert.Empty(t, s.asks)

	r := gentesttest.New(t, gentesttest.WithReferenceModels(""))
	req := imageReq(string(gentesttest.EditModel))
	req.References = stage(t, r, "face")
	ref, err := r.Confirmed(req)
	require.NoError(t, err)
	got, err := r.Svc.Result(context.Background(), r.Wait(ref.BatchID).Results[0].ID)
	require.NoError(t, err)
	assert.Nil(t, got.FaceSimilarity, "the face models are not installed: no number")
}
