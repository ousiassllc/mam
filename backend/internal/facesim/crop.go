package facesim

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg" // the decoders of the pictures CropFace takes
	"image/png"

	_ "golang.org/x/image/webp"
)

// The shape of the crop CropFace makes (#960).
const (
	// cropScale is the side of the crop in sides of the detected face. The detector's box runs from the brows to the chin; twice its
	// larger side takes in the forehead, the hair line, the ears and the neck, which is what the identity of a face is read from,
	// and leaves out the clothes and the background that a reference "of the face only" is not meant to bring.
	cropScale = 2.0
	// cropLift moves the centre of the crop up from the centre of the box, in heights of the box: the box lacks the forehead.
	cropLift = 0.1
	// minCropSide is the least side of a crop, in pixels: a face smaller than this has no more to give than the whole picture.
	minCropSide = 64
	// wholeShare is the share of the picture a crop may cover before it is not worth cutting: the picture is a close-up already.
	wholeShare = 0.85
)

// CropFace cuts the largest face of the picture (an image file: PNG, JPEG or WebP) out with some of its surroundings and returns the
// crop as PNG. ok is false, and cropped nil, when the picture has no face, when it is a close-up of the face already (the crop would
// be most of it), or when the face is smaller than minCropSide: the caller then uses the picture as it is. An engine that fails is
// the error; so is a file that does not decode.
//
// The face reference of an editing workflow is read by a vision tower that looks at the whole picture: what is not the face (the
// hair, the clothes, the room) is noise to the identity, and the picture is shrunk to the size the tower reads, so that a face that
// is small in the picture is read at a fraction of its detail.
func CropFace(ctx context.Context, eng Engine, data []byte) (cropped []byte, ok bool, err error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, false, fmt.Errorf("facesim: decode the reference: %w", err)
	}
	faces, err := eng.Faces(ctx, img)
	if err != nil {
		return nil, false, err
	}
	if len(faces) == 0 {
		return nil, false, nil
	}
	r, ok := cropRect(faces[0].Box, img.Bounds())
	if !ok {
		return nil, false, nil
	}
	out := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(out, out.Bounds(), img, r.Min, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, false, fmt.Errorf("facesim: encode the crop: %w", err)
	}
	return buf.Bytes(), true, nil
}

// cropRect is the square around the face box, inside bounds: moved, not shrunk, when it would leave the picture (a face at the edge
// is not centred in its crop), and shrunk only when the picture is smaller than the square. ok is false when it is not worth
// cutting (see CropFace).
func cropRect(box, bounds image.Rectangle) (image.Rectangle, bool) {
	side := int(float64(max(box.Dx(), box.Dy())) * cropScale)
	side = min(side, bounds.Dx(), bounds.Dy())
	if side < minCropSide {
		return image.Rectangle{}, false
	}
	cx := (box.Min.X + box.Max.X) / 2
	cy := (box.Min.Y+box.Max.Y)/2 - int(float64(box.Dy())*cropLift)
	x := min(max(cx-side/2, bounds.Min.X), bounds.Max.X-side)
	y := min(max(cy-side/2, bounds.Min.Y), bounds.Max.Y-side)
	r := image.Rect(x, y, x+side, y+side)
	if float64(r.Dx()*r.Dy()) >= wholeShare*float64(bounds.Dx()*bounds.Dy()) {
		return image.Rectangle{}, false
	}
	return r, true
}
