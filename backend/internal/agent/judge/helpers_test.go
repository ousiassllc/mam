package judge_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/judge"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/facesim"
)

// The fake face engine reads the picture itself: the red channel of the top-left pixel says which
// face is in it. Red r stands for the unit embedding (cos a, sin a) with a = r/255 · π/2, so the
// cosine similarity of two pictures is cos(a1 − a2). Picture red 0 is the reference face. A
// picture whose green channel is 255 has no face.

// shade is the red value of a face whose similarity to the reference (red 0) is sim.
func shade(sim float64) uint8 { return uint8(math.Round(math.Acos(sim) / (math.Pi / 2) * 255)) }

// picture is a JPEG of a solid colour.
func picture(t testing.TB, r, g uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			img.Set(x, y, color.RGBA{R: r, G: g, B: 0, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, &jpeg.Options{Quality: 100}))
	return buf.Bytes()
}

func facePicture(t testing.TB, sim float64) []byte { return picture(t, shade(sim), 0) }
func noFacePicture(t testing.TB) []byte            { return picture(t, 0, 255) }

func embeddingOf(r uint8) facesim.Embedding {
	a := float64(r) / 255 * math.Pi / 2
	return facesim.Embedding{float32(math.Cos(a)), float32(math.Sin(a))}
}

// fakeEngine implements facesim.Engine over the pictures above.
type fakeEngine struct {
	mu    sync.Mutex
	calls int
	err   error
	// faces overrides the reading of a picture (multi-face frames).
	faces func(img image.Image) []facesim.Face
}

func (e *fakeEngine) Faces(_ context.Context, img image.Image) ([]facesim.Face, error) {
	e.mu.Lock()
	e.calls++
	e.mu.Unlock()
	if e.err != nil {
		return nil, e.err
	}
	if e.faces != nil {
		return e.faces(img), nil
	}
	r, g, _, _ := img.At(0, 0).RGBA()
	if g>>8 > 200 {
		return nil, nil
	}
	return []facesim.Face{{Box: image.Rect(0, 0, 8, 8), Score: 0.9, Embedding: embeddingOf(uint8(r >> 8))}}, nil
}

// fakeFrames is a FrameSource whose frames are the given pictures, one per interval.
type fakeFrames struct {
	mu       sync.Mutex
	pictures [][]byte
	duration float64
	fps      float64
	specs    []judge.SampleSpec
	err      error
}

func (f *fakeFrames) Sample(_ context.Context, _ string, spec judge.SampleSpec) (judge.Clip, error) {
	f.mu.Lock()
	f.specs = append(f.specs, spec)
	f.mu.Unlock()
	if f.err != nil {
		return judge.Clip{}, f.err
	}
	clip := judge.Clip{Duration: f.duration, FPS: f.fps, Interval: f.duration / float64(len(f.pictures))}
	for i, p := range f.pictures {
		tm := float64(i) * clip.Interval
		n, _ := clip.FrameRange(tm, tm)
		clip.Frames = append(clip.Frames, judge.Frame{Time: tm, Number: n, JPEG: p})
	}
	return clip, nil
}

// fakeLLM answers every call with the same text and records the requests.
type fakeLLM struct {
	mu       sync.Mutex
	text     string
	err      error
	requests []llm.Request
	agents   []llm.Agent
}

func (f *fakeLLM) Complete(_ context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	f.agents = append(f.agents, agent)
	if f.err != nil {
		return llm.Response{}, f.err
	}
	return llm.Response{Text: f.text, Provider: llm.ProviderGoogle, Model: "fake"}, nil
}

func writeFile(path string, data []byte) error { return os.WriteFile(path, data, 0o600) }
