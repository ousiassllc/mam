package main

import (
	"errors"
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/agent/judge"
	"github.com/ousiassllc/mam/backend/internal/config"
	"github.com/ousiassllc/mam/backend/internal/facesim"
)

// judgeSetup is what the AI Video Judge needs from the machine: ffmpeg to sample frames and, for
// the Face Identity Similarity, the local face models.
type judgeSetup struct {
	config judge.Config
	// frames is nil when ffmpeg is missing: the Judge is then off.
	frames judge.FrameSource
	// faces is nil when the models or the ONNX Runtime library are missing: Takes are then judged
	// qualitatively only.
	faces *facesim.ONNX
}

// newJudgeSetup loads the Judge setting and looks for ffmpeg and the face models. Missing tools
// are not fatal: the reduced feature set is logged.
func newJudgeSetup(cfg config.Config, logger *slog.Logger) (judgeSetup, error) {
	s := judgeSetup{}
	var err error
	if s.config, err = judge.LoadConfig(cfg.JudgeConfigPath); err != nil {
		return s, err
	}
	if ff, err := judge.LookFFmpeg(); err != nil {
		logger.Warn("AI Video Judge is off: ffmpeg / ffprobe not found")
	} else {
		s.frames = ff
	}
	faces, err := facesim.NewONNX(facesim.ONNXConfig{ModelDir: cfg.FaceModels(), RuntimeLibrary: cfg.ONNXRuntimeLib})
	switch {
	case err == nil:
		s.faces = faces
	case errors.Is(err, facesim.ErrUnavailable):
		logger.Warn("face identity similarity is off: Takes are judged qualitatively only", "model_dir", cfg.FaceModels(), "err", err)
	default:
		return s, err
	}
	return s, nil
}

// close releases the face models.
func (s judgeSetup) close() {
	if s.faces != nil {
		_ = s.faces.Close()
	}
}

// engine is the face engine as an interface value that is nil (not a typed nil) when unavailable.
func (s judgeSetup) engine() facesim.Engine {
	if s.faces == nil {
		return nil
	}
	return s.faces
}
