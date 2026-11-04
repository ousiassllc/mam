package screentest_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/facesim"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/face"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Issue #1026: a result made from a face reference while the face models are missing says on its detail that the similarity is off, why and what to put where.
func TestTheDetailSaysTheFaceSimilarityIsOffWhenTheFaceModelsAreMissing(t *testing.T) {
	off := face.OffRecord(&facesim.Off{Reason: facesim.ReasonModelsMissing, ModelDir: "/data/models", Missing: []string{facesim.DetectorFile, facesim.RecognizerFile}})
	s := newScreen(t, gentesttest.WithFaceFusion(fakeSwap, nil), gentesttest.WithFaceOff(off))
	src := s.anImage()
	before := len(s.batches())

	rec := s.upload(paths.GenerationTestPart("results", src, "face-swap"), url.Values{"swapper": {"inswapper_128"}, "enhancer": {"none"}}, "face", gentesttest.PNG)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	var made string
	for _, b := range s.batches() {
		if rs := s.results(b.BatchID); len(rs) == 1 && rs[0].Model == "facefusion" {
			s.rig.Wait(b.BatchID)
			made = s.results(b.BatchID)[0].ID
		}
	}
	require.Len(t, s.batches(), before+1)
	require.NotEmpty(t, made)

	body := s.page(paths.GenerationTestPart("results", made))
	assert.Contains(t, body, "顔の類似度")
	assert.Contains(t, body, "顔の類似度はオフ")
	assert.Contains(t, body, facesim.RecognizerFile, "which file is missing")
	assert.Contains(t, body, "/data/models", "where to put it")
}
