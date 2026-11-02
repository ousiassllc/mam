package gentesttest

import (
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/runner"
)

// WithFaceCrop gives the service the cut of a face reference to its face (the application's is facesim.CropFace over the face models).
func WithFaceCrop(f runner.FaceCropFunc) Option {
	return func(_ *Rig, d *gentest.Deps) { d.FaceCrop = f }
}
