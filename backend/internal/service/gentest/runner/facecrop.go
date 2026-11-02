package runner

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// FaceCropFunc cuts the largest face of a picture (an image file) out with its surroundings and returns the crop as PNG; ok is false
// when the picture is better left as it is (no face, a close-up already). facesim.CropFace is the one the application gives.
type FaceCropFunc func(ctx context.Context, image []byte) (cropped []byte, ok bool, err error)

// cropFace is the picture of a reference of the role "face" cut to the face (#960), nil for any other role, when there is no
// FaceCrop, when the picture has no face worth cutting to, and when the cut fails: a reference whose face cannot be found is used
// whole, as it was before the cut, and the failure is only logged.
func (s *Runner) cropFace(ctx context.Context, l loaded) *modeladapter.Input {
	if s.FaceCrop == nil || l.role != localcfg.RoleFace {
		return nil
	}
	cropped, ok, err := s.FaceCrop(ctx, l.in.Data)
	if err != nil {
		s.Logger.Warn("cut a face reference to its face failed: the picture is used whole", "err", err)
		return nil
	}
	if !ok {
		return nil
	}
	return &modeladapter.Input{Data: cropped, MimeType: "image/png"}
}

// forRole is the picture a model gets for the reference when the model takes it as role: the face crop when the model reads it as a
// face, the picture itself otherwise (a model that does not take the role uses the whole picture, with the whole of it).
func (l loaded) forRole(role string) modeladapter.Input {
	if role == localcfg.RoleFace && l.face != nil {
		return *l.face
	}
	return l.in
}
