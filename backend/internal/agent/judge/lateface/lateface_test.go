// Package lateface tests the Judge's face measurement over an engine whose models are installed while the app runs (Issue #1026), with fakes of its own (the
// judge package's directory is over the line limit and cannot take more tests).
package lateface_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/judge"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/facesim"
)

const passAnswer = `{"result":"pass","overall_confidence":0.93,"issues":[],"preserve":["character"]}`

type fakeLLM struct{}

func (fakeLLM) Complete(context.Context, llm.Agent, llm.Request) (llm.Response, error) {
	return llm.Response{Text: passAnswer, Provider: llm.ProviderGoogle, Model: "fake"}, nil
}

// frames samples four identical pictures and counts how often it was asked.
type frames struct {
	jpeg  []byte
	calls atomic.Int32
}

func (f *frames) Sample(context.Context, string, judge.SampleSpec) (judge.Clip, error) {
	f.calls.Add(1)
	clip := judge.Clip{Duration: 4, FPS: 24, Interval: 1}
	for i := range 4 {
		n, _ := clip.FrameRange(float64(i), float64(i))
		clip.Frames = append(clip.Frames, judge.Frame{Time: float64(i), Number: n, JPEG: f.jpeg})
	}
	return clip, nil
}

// lateEngine finds the same face in every picture, once its models are installed (on), and is off before.
type lateEngine struct {
	on    atomic.Bool
	calls atomic.Int32
}

func (e *lateEngine) Faces(context.Context, image.Image) ([]facesim.Face, error) {
	if !e.on.Load() {
		return nil, facesim.ErrUnavailable
	}
	e.calls.Add(1)
	return []facesim.Face{{Box: image.Rect(0, 0, 8, 8), Score: 0.9, Embedding: facesim.Embedding{1, 0}}}, nil
}

func (e *lateEngine) Available() bool { return e.on.Load() }

func solid(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			img.Set(x, y, color.RGBA{R: 40, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, &jpeg.Options{Quality: 100}))
	return buf.Bytes()
}

// The Judge asks the engine every time: while the models are not installed the Take is judged qualitatively and no face frames are sampled for it, and the
// Take after the install is measured by the same Judge, without a restart.
func TestEvaluate_TheFaceMeasurementStartsWhenTheModelsAreInstalledWhileTheAppRuns(t *testing.T) {
	pic := solid(t)
	fr := &frames{jpeg: pic}
	engine := &lateEngine{}
	j := judge.New(fakeLLM{}, fr, engine, judge.DefaultConfig(), nil)
	in := judge.Input{
		VideoPath:    "take.mp4",
		Spec:         domain.ShotSpec{ShotObjective: "A woman runs", Camera: domain.SpecCamera{Framing: "medium"}},
		FaceSubjects: []judge.FaceSubject{{ID: "char-a", ReferenceImages: [][]byte{pic}}},
	}

	off, err := j.Evaluate(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, domain.FaceUnavailable, off.Metrics.FaceIdentityStatus)
	sampledWhileOff := fr.calls.Load()
	assert.Equal(t, int32(1), sampledWhileOff, "only the review frames were sampled: none for the face measurement of an engine that is off")
	assert.Zero(t, engine.calls.Load())

	engine.on.Store(true) // the models are installed from the model manager

	on, err := j.Evaluate(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, domain.FaceMeasured, on.Metrics.FaceIdentityStatus)
	assert.NotEmpty(t, on.Metrics.FaceIdentity)
	assert.Greater(t, fr.calls.Load(), sampledWhileOff+1, "the face frames are sampled now")
}
