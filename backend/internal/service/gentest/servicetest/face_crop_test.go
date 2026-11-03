package servicetest_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
)

// cropper is a FaceCropFunc that answers with a mark, and records the pictures it was asked about.
type cropper struct {
	mu   sync.Mutex
	asks [][]byte
	ok   bool
	err  error
}

var faceMark = []byte("cut to the face")

func (c *cropper) crop(_ context.Context, image []byte) ([]byte, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.asks = append(c.asks, image)
	return faceMark, c.ok && c.err == nil, c.err
}

// run sends one run of the three roles to the editing model and the img2img model, and returns what each model got.
func runWithFace(t *testing.T, c *cropper) (edit, i2i []byte, editInputs [][]byte) {
	t.Helper()
	r := gentesttest.New(t, gentesttest.WithReferenceModels(""), gentesttest.WithFaceCrop(c.crop))
	req := imageReq(string(gentesttest.EditModel), string(gentesttest.Img2ImgModel))
	req.References = stage(t, r, "face", "style", "reference")
	ref, err := r.Confirmed(req)
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	require.Equal(t, gentest.StateSucceeded, st.State, "%+v", st.Results)

	e := r.Fake.CallsOf(gentesttest.EditModel)[0].Image
	for _, in := range e.Inputs {
		editInputs = append(editInputs, in.Data)
	}
	return editInputs[0], r.Fake.CallsOf(gentesttest.Img2ImgModel)[0].Image.Inputs[0].Data, editInputs
}

// An editing model gets the cut of a "face only" reference; the other references and a model that does not take the role (it uses
// the picture as a whole one) get the picture as it was.
func TestRunCutsAFaceReferenceForAnEditingModelOnly(t *testing.T) {
	c := &cropper{ok: true}
	face, i2i, all := runWithFace(t, c)

	assert.Equal(t, faceMark, face, "the face of an editing model is the cut")
	assert.NotEqual(t, faceMark, all[1], "a style reference is not cut")
	assert.NotEqual(t, faceMark, all[2], "a whole picture is not cut")
	assert.NotEqual(t, faceMark, i2i, "a model that uses the picture as a whole is given the whole of it")
	assert.Len(t, c.asks, 1, "only the picture of the face is asked about, once for the batch")
}

// A picture whose face is not found, and a detector that fails, leave the run as it was: the picture goes whole.
func TestRunSendsTheWholePictureWhenTheFaceCannotBeCut(t *testing.T) {
	for name, c := range map[string]*cropper{
		"no face to cut to":  {ok: false},
		"the detector fails": {err: errors.New("the model failed")},
	} {
		t.Run(name, func(t *testing.T) {
			face, _, _ := runWithFace(t, c)
			assert.NotEqual(t, faceMark, face)
			assert.NotEmpty(t, face)
		})
	}
}

// Without the cut (the face models are not there) the run is what it was.
func TestRunWithoutAFaceCutSendsThePicture(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithReferenceModels(""))
	req := imageReq(string(gentesttest.EditModel))
	req.References = stage(t, r, "face")
	ref, err := r.Confirmed(req)
	require.NoError(t, err)
	require.Equal(t, gentest.StateSucceeded, r.Wait(ref.BatchID).State)
	got := r.Fake.CallsOf(gentesttest.EditModel)[0].Image.Inputs[0].Data
	assert.Equal(t, append(append([]byte(nil), gentesttest.PNG...), 0), got)
}
