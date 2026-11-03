package face_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/facefusion"
	"github.com/ousiassllc/mam/backend/internal/facesim"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/face"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/runner"
)

func imageReq(models ...string) gentest.Request {
	return gentest.Request{Media: "image", Models: models, Prompts: []gentest.PromptInput{{Prompt: "a fox"}}}
}

// stage stages n different uploads (a staged id is consumed by a run) and returns refs of the roles given.
func stage(t *testing.T, r *gentesttest.Rig, roles ...string) []gentest.RefInput {
	t.Helper()
	var out []gentest.RefInput
	for i, role := range roles {
		id, _, err := r.Svc.StageReference(append(append([]byte(nil), gentesttest.PNG...), byte(i)))
		require.NoError(t, err)
		out = append(out, gentest.RefInput{Source: gentest.FromStaged, ID: id, Role: role})
	}
	return out
}

// swapper is a FaceSwapFunc that records what it was asked and answers a picture (or fails).
type swapper struct {
	mu    sync.Mutex
	asked []runner.FaceSwapInput
	out   []byte
	err   error
}

func (s *swapper) swap(_ context.Context, in runner.FaceSwapInput) ([]byte, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked = append(s.asked, in)
	return s.out, "image/png", s.err
}

var swapped = append(append([]byte(nil), gentesttest.PNG...), 7)

// swapRig is a rig with FaceFusion and the measurement, and a picture of the history to swap into and a staged face picture.
func swapRig(t *testing.T, s *swapper, sc *scorer, check func() error) (r *gentesttest.Rig, target, faceRef gentest.RefInput) {
	t.Helper()
	r = gentesttest.New(t, gentesttest.WithFaceFusion(s.swap, check), gentesttest.WithFaceScore(sc.score))
	ref, err := r.Confirmed(imageReq("flux-2"))
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	require.Equal(t, gentest.StateSucceeded, st.State)
	target = gentest.RefInput{Source: gentest.FromHistory, ID: st.Results[0].ID}
	faceRef = stage(t, r, "face")[0]
	return r, target, faceRef
}

func swapDone(t *testing.T, r *gentesttest.Rig, ref face.SwapRef) gentest.BatchStatus {
	t.Helper()
	st := r.Wait(ref.BatchID)
	require.Len(t, st.Results, 1)
	return st
}

// A swap is a batch of its own with one result of the model "facefusion": FaceFusion gets the target picture and the face picture with the
// default models (GHOST, YuNet, GFPGAN), and the picture it returns is stored as a file of the history, beside the one it was swapped into.
func TestFaceSwapRunsFaceFusionAndStoresItsPictureAsAResult(t *testing.T) {
	s := &swapper{out: swapped}
	r, target, faceRef := swapRig(t, s, &scorer{cmp: facesim.Comparison{Cosine: 0.6}}, nil)

	ref, err := face.Swap(context.Background(), r.Svc, face.SwapRequest{Target: target, Face: faceRef})
	require.NoError(t, err)
	st := swapDone(t, r, ref)

	require.Equal(t, gentest.StateSucceeded, st.State, "%+v", st.Results)
	assert.NotEmpty(t, ref.JobID)
	assert.Empty(t, ref.Warnings, "the defaults need no warning")
	res := st.Results[0]
	assert.Equal(t, ref.ResultID, res.ID)
	assert.Equal(t, face.SwapModel, res.Model)
	assert.True(t, res.HasFile)
	require.Len(t, s.asked, 1)
	in := s.asked[0]
	assert.Equal(t, facefusion.Selection{Swapper: "ghost_2_256", Detector: "yunet", Enhancer: "gfpgan_1.4"}, facefusion.Selection{Swapper: in.Swapper, Detector: in.Detector, Enhancer: in.Enhancer})
	assert.Equal(t, gentesttest.PNG, in.Target.Data, "the picture of the history result")
	assert.Equal(t, append(append([]byte(nil), gentesttest.PNG...), 0), in.Face.Data, "the staged face")

	d, err := r.Svc.Result(context.Background(), ref.ResultID)
	require.NoError(t, err)
	require.NotNil(t, d.FaceSwap)
	assert.Equal(t, target.ID, d.FaceSwap.SourceResultID)
	assert.Equal(t, "ghost_2_256", d.FaceSwap.Swapper)
	assert.Equal(t, "gfpgan_1.4", d.FaceSwap.Enhancer)
	require.NotNil(t, d.FaceSimilarity, "the face of the swap is held to the face picture as any result of the role face")
	assert.InDelta(t, 0.6, d.FaceSimilarity.Cosine, 1e-9)
	assert.Len(t, d.Inputs, 2)
	roles := []string{d.Inputs[0].Role, d.Inputs[1].Role}
	assert.ElementsMatch(t, []string{"edit", "face"}, roles)

	file, err := r.Svc.ResultFile(context.Background(), ref.ResultID)
	require.NoError(t, err)
	got, err := os.ReadFile(filepath.Clean(file.Path))
	require.NoError(t, err)
	assert.Equal(t, swapped, got)
}

// A model the user chose that the license does not clear is used, and the warning comes back and stays on the result.
func TestFaceSwapWithANonCommercialModelWarnsAndRemembersTheWarning(t *testing.T) {
	s := &swapper{out: swapped}
	r, target, faceRef := swapRig(t, s, &scorer{}, nil)

	ref, err := face.Swap(context.Background(), r.Svc, face.SwapRequest{
		Target: target, Face: faceRef, Selection: facefusion.Selection{Swapper: "inswapper_128", Enhancer: facefusion.EnhancerNone},
	})
	require.NoError(t, err)
	swapDone(t, r, ref)

	require.Len(t, ref.Warnings, 1)
	assert.Contains(t, ref.Warnings[0], "inswapper_128")
	assert.Contains(t, ref.Warnings[0], "商用NG")
	d, err := r.Svc.Result(context.Background(), ref.ResultID)
	require.NoError(t, err)
	assert.Equal(t, ref.Warnings, d.FaceSwap.Warnings)
	assert.Empty(t, d.FaceSwap.Enhancer)
	assert.Equal(t, facefusion.EnhancerNone, s.asked[0].Enhancer)
}

func TestFaceSwapRefusesWhatCannotRunBeforeMakingAnything(t *testing.T) {
	s := &swapper{out: swapped}
	r, target, faceRef := swapRig(t, s, &scorer{}, nil)
	var invalid *gterr.InvalidError

	_, err := face.Swap(context.Background(), r.Svc, face.SwapRequest{Target: target, Face: faceRef, Selection: facefusion.Selection{Swapper: "nope"}})
	require.ErrorAs(t, err, &invalid)
	assert.Contains(t, invalid.Message, "nope")

	_, err = face.Swap(context.Background(), r.Svc, face.SwapRequest{Target: gentest.RefInput{Source: gentest.FromHistory, ID: "gone"}, Face: faceRef})
	require.ErrorAs(t, err, &invalid, "a target that is not there")

	_, err = face.Swap(context.Background(), r.Svc, face.SwapRequest{Target: target, Face: gentest.RefInput{Source: gentest.FromStaged, ID: "gone-gone-gone.png"}})
	require.ErrorAs(t, err, &invalid, "a face that is not there")
	assert.Empty(t, s.asked, "FaceFusion was never started")
}

func TestFaceSwapSaysWhyFaceFusionCannotRun(t *testing.T) {
	var invalid *gterr.InvalidError
	r, target, faceRef := swapRig(t, &swapper{}, &scorer{}, func() error { return errors.New("python not found (searched /usr/bin)") })

	_, err := face.Swap(context.Background(), r.Svc, face.SwapRequest{Target: target, Face: faceRef})

	require.ErrorAs(t, err, &invalid)
	assert.Contains(t, invalid.Message, "python not found")
	assert.Contains(t, invalid.Message, "設定")

	bare := gentesttest.New(t)
	err = face.Available(bare.Svc)
	require.ErrorAs(t, err, &invalid, "an application without FaceFusion")
}

// FaceFusion failing (a face it cannot find, its refusal of an adult picture) is the failure of the result, in the words it printed.
func TestFaceSwapThatFailsIsAFailedResultWithTheReason(t *testing.T) {
	s := &swapper{err: &facefusion.RunError{Err: errors.New("exit status 1"), Output: "NSFW content detected"}}
	r, target, faceRef := swapRig(t, s, &scorer{}, nil)

	ref, err := face.Swap(context.Background(), r.Svc, face.SwapRequest{Target: target, Face: faceRef})
	require.NoError(t, err)
	st := swapDone(t, r, ref)

	assert.Equal(t, gentest.StateFailed, st.State)
	assert.False(t, st.Results[0].HasFile)
	assert.Contains(t, st.Results[0].Error, "NSFW content detected")
}
