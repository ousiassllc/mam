package facesim

import (
	"context"
	"fmt"
	"image"
)

// ONNX is not available on Windows: the ONNX Runtime bindings do not support it. NewONNX always
// returns ErrUnavailable there, so the Judge falls back to qualitative judging.
type ONNX struct{}

// NewONNX implements the non-Windows constructor.
func NewONNX(ONNXConfig) (*ONNX, error) {
	return nil, fmt.Errorf("%w: the ONNX Runtime bindings do not support Windows", ErrUnavailable)
}

// Close implements Engine.
func (*ONNX) Close() error { return nil }

// Faces implements Engine.
func (*ONNX) Faces(context.Context, image.Image) ([]Face, error) {
	return nil, ErrUnavailable
}
