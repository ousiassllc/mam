package screentest_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
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

// Issue #1026: the screen of the input images says the face similarity is off and links to the model manager's "顔チェック用モデル" while the models are missing;
// it says nothing while the similarity works, and does not offer the models as the fix when the ONNX Runtime library is what is missing.
func TestTheInputImagesLinkToTheFaceModelsWhileTheSimilarityIsOff(t *testing.T) {
	modelsOff := face.OffRecord(&facesim.Off{Reason: facesim.ReasonModelsMissing, ModelDir: "/data/models", Missing: []string{facesim.DetectorFile, facesim.RecognizerFile}})
	body := newScreen(t, gentesttest.WithFaceOff(modelsOff)).page(paths.GenerationTest())
	assert.Contains(t, body, `data-testid="gt-face-off"`)
	assert.Contains(t, body, "顔チェック用モデルを入れる")
	assert.Contains(t, body, `href="/model-manager/face-check#mm-face-models"`, "the section is on the page of the face-check models")
	assert.Equal(t, paths.ModelManagerFaceModels(), "/model-manager/face-check#mm-face-models")
	assert.Equal(t, 1, strings.Count(body, `data-testid="gt-face-off-install"`))

	runtimeOff := face.OffRecord(&facesim.Off{Reason: facesim.ReasonRuntimeMissing, ModelDir: "/data/models", Detail: "onnxruntime.dll: not found"})
	body = newScreen(t, gentesttest.WithFaceOff(runtimeOff)).page(paths.GenerationTest())
	assert.Contains(t, body, `data-testid="gt-face-off"`)
	assert.NotContains(t, body, "顔チェック用モデルを入れる", "installing the models does not fix a missing ONNX Runtime library")
	assert.Contains(t, body, "ONNX Runtime")

	body = newScreen(t, gentesttest.WithFaceScore(func(context.Context, []byte, []byte) (facesim.Comparison, error) {
		return facesim.Comparison{}, nil
	})).page(paths.GenerationTest())
	assert.NotContains(t, body, `data-testid="gt-face-off"`, "the similarity works: nothing to say")
	assert.NotContains(t, newScreen(t).page(paths.GenerationTest()), `data-testid="gt-face-off"`, "no face similarity at all: nothing to say")
}
