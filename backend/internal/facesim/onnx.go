//go:build !windows

// The ONNX Runtime bindings (onnxruntime-purego) load the library with dlopen and do not build for
// Windows; onnx_windows.go answers ErrUnavailable there, so the Judge falls back to qualitative judging.

package facesim

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"sort"
	"strconv"
	"sync"

	ort "github.com/shota3506/onnxruntime-purego/onnxruntime"
)

// ortAPIVersion is the ONNX Runtime C API version the bindings use (ONNX Runtime 1.23.x).
const ortAPIVersion = 23

// ONNX is the Engine backed by YuNet and SFace running on the ONNX Runtime.
type ONNX struct {
	rt       *ort.Runtime
	env      *ort.Env
	detector *ort.Session
	embedder *ort.Session
	minScore float32
	// mu serialises inference: one image at a time keeps memory use bounded.
	mu sync.Mutex
}

// NewONNX loads the models. It returns an error wrapping ErrUnavailable when a model file or the
// runtime library is missing, so that callers can fall back to qualitative judging.
func NewONNX(cfg ONNXConfig) (*ONNX, error) {
	for _, p := range []string{cfg.detector(), cfg.recognizer()} {
		if _, err := os.Stat(p); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
	}
	rt, err := ort.NewRuntime(cfg.RuntimeLibrary, ortAPIVersion)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	e := &ONNX{rt: rt, minScore: cfg.MinScore}
	if e.minScore <= 0 {
		e.minScore = 0.6
	}
	fail := func(err error) (*ONNX, error) {
		_ = e.Close()
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if e.env, err = rt.NewEnv("mam-facesim", ort.LoggingLevelError); err != nil {
		return fail(err)
	}
	opts := &ort.SessionOptions{IntraOpNumThreads: 2}
	if e.detector, err = rt.NewSession(e.env, cfg.detector(), opts); err != nil {
		return fail(err)
	}
	if e.embedder, err = rt.NewSession(e.env, cfg.recognizer(), opts); err != nil {
		return fail(err)
	}
	return e, nil
}

// Close releases the models and the runtime.
func (e *ONNX) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.embedder != nil {
		e.embedder.Close()
		e.embedder = nil
	}
	if e.detector != nil {
		e.detector.Close()
		e.detector = nil
	}
	if e.env != nil {
		e.env.Close()
		e.env = nil
	}
	if e.rt != nil {
		err := e.rt.Close()
		e.rt = nil
		return err
	}
	return nil
}

// Faces implements Engine.
func (e *ONNX) Faces(ctx context.Context, img image.Image) ([]Face, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.rt == nil {
		return nil, errors.New("facesim: engine is closed")
	}
	m := toRGB(img, maxImageSide)
	scale := float32(detectorSize) / float32(max(m.w, m.h))
	dets, err := e.detect(ctx, m, scale)
	if err != nil {
		return nil, err
	}
	faces := make([]Face, 0, len(dets))
	b := img.Bounds()
	for _, d := range dets {
		emb, err := e.embed(ctx, m, d, scale)
		if err != nil {
			return nil, err
		}
		toImage := func(v float32) float32 { return v / scale * float32(m.step) }
		faces = append(faces, Face{
			Box: image.Rect(
				b.Min.X+int(toImage(d.x1)), b.Min.Y+int(toImage(d.y1)), b.Min.X+int(toImage(d.x2)), b.Min.Y+int(toImage(d.y2)),
			).Intersect(b),
			Score:     d.score,
			Embedding: emb,
		})
	}
	sort.SliceStable(faces, func(i, j int) bool { return faces[i].Box.Dx()*faces[i].Box.Dy() > faces[j].Box.Dx()*faces[j].Box.Dy() })
	return faces, nil
}

// detect runs YuNet on m, letterboxed into the top-left of the square input.
func (e *ONNX) detect(ctx context.Context, m rgb, scale float32) ([]detection, error) {
	plane := detectorSize * detectorSize
	in := make([]float32, 3*plane)
	w, h := min(int(float32(m.w)*scale+0.5), detectorSize), min(int(float32(m.h)*scale+0.5), detectorSize)
	for y := range h {
		for x := range w {
			sx, sy := (float64(x)+0.5)/float64(scale)-0.5, (float64(y)+0.5)/float64(scale)-0.5
			// YuNet takes BGR.
			in[y*detectorSize+x] = m.sample(sx, sy, 2)
			in[plane+y*detectorSize+x] = m.sample(sx, sy, 1)
			in[2*plane+y*detectorSize+x] = m.sample(sx, sy, 0)
		}
	}
	input, err := ort.NewTensorValue(e.rt, in, []int64{1, 3, detectorSize, detectorSize})
	if err != nil {
		return nil, fmt.Errorf("facesim: detector input: %w", err)
	}
	defer input.Close()
	outs, err := e.detector.Run(ctx, map[string]*ort.Value{e.detector.InputNames()[0]: input})
	if err != nil {
		return nil, fmt.Errorf("facesim: detect: %w", err)
	}
	defer func() {
		for _, v := range outs {
			v.Close()
		}
	}()
	raw := yunetOutputs{cls: map[int][]float32{}, obj: map[int][]float32{}, bbox: map[int][]float32{}, kps: map[int][]float32{}}
	for _, s := range yunetStrides {
		for name, dst := range map[string]map[int][]float32{"cls": raw.cls, "obj": raw.obj, "bbox": raw.bbox, "kps": raw.kps} {
			v, ok := outs[name+"_"+strconv.Itoa(s)]
			if !ok {
				return nil, fmt.Errorf("facesim: detector has no output %s_%d", name, s)
			}
			data, _, err := ort.GetTensorData[float32](v)
			if err != nil {
				return nil, fmt.Errorf("facesim: detector output %s_%d: %w", name, s, err)
			}
			dst[s] = data
		}
	}
	return decodeYuNet(raw, detectorSize, e.minScore, nmsIoU)
}

// embed aligns the face and runs SFace on it.
func (e *ONNX) embed(ctx context.Context, m rgb, d detection, scale float32) (Embedding, error) {
	input, err := ort.NewTensorValue(e.rt, alignFace(m, d, scale), []int64{1, 3, sfaceSize, sfaceSize})
	if err != nil {
		return nil, fmt.Errorf("facesim: recognizer input: %w", err)
	}
	defer input.Close()
	outs, err := e.embedder.Run(ctx, map[string]*ort.Value{e.embedder.InputNames()[0]: input})
	if err != nil {
		return nil, fmt.Errorf("facesim: embed: %w", err)
	}
	defer func() {
		for _, v := range outs {
			v.Close()
		}
	}()
	v, ok := outs[e.embedder.OutputNames()[0]]
	if !ok {
		return nil, errors.New("facesim: recognizer returned no output")
	}
	data, _, err := ort.GetTensorData[float32](v)
	if err != nil {
		return nil, fmt.Errorf("facesim: recognizer output: %w", err)
	}
	normalise(data)
	return Embedding(data), nil
}
