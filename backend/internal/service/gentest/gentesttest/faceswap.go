package gentesttest

import (
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/facemodels"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/runner"
)

// WithFaceScore gives the service the measurement of the face similarity of a result (the application's is facesim.Compare over the face models).
func WithFaceScore(f runner.FaceScoreFunc) Option {
	return func(_ *Rig, d *gentest.Deps) { d.FaceScore = f }
}

// WithFaceOff gives the service the reason its face similarity is off, for good (the application's is face.OffRecord of why the face models are not there).
func WithFaceOff(off *record.FaceSimilarity) Option {
	return WithFaceOffFunc(func() *record.FaceSimilarity { return off })
}

// WithFaceOffFunc gives the service the reason its face similarity is off as it is now (the application's is face.OffNow: the models can be installed while it runs).
func WithFaceOffFunc(f runner.FaceOffFunc) Option {
	return func(_ *Rig, d *gentest.Deps) { d.FaceOff = f }
}

// WithFaceModels gives the service the installer of the face check models (the application's is facemodels.Service over the model folder the face engine reads).
func WithFaceModels(m *facemodels.Service) Option {
	return func(_ *Rig, d *gentest.Deps) { d.FaceModels = m }
}

// WithFaceFusion gives the service a FaceFusion (the application's runs `facefusion.py headless-run` of the install the settings find). check
// is the answer of the lookup: nil is found, an error is why it is not (nil check: always found).
func WithFaceFusion(f runner.FaceSwapFunc, check func() error) Option {
	return func(_ *Rig, d *gentest.Deps) { d.FaceFusion, d.FaceFusionCheck = f, check }
}
