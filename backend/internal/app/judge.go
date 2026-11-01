package app

import (
	"errors"
	"fmt"
	"log/slog"
	"runtime"

	"github.com/ousiassllc/mam/backend/internal/agent/judge"
	"github.com/ousiassllc/mam/backend/internal/config"
	"github.com/ousiassllc/mam/backend/internal/facesim"
	"github.com/ousiassllc/mam/backend/internal/media"
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
// tools is the ffmpeg and ffprobe the app found at startup (nil: not found, and logged by the caller).
func newJudgeSetup(cfg config.Config, tools *media.Tools, logger *slog.Logger) (judgeSetup, error) {
	s := judgeSetup{}
	var err error
	if s.config, err = judge.LoadConfig(cfg.JudgeConfigPath); err != nil {
		return s, err
	}
	if tools == nil {
		logger.Warn("AI Video Judge is off: ffmpeg / ffprobe not found")
	} else {
		s.frames = judge.NewFFmpeg(*tools)
	}
	faces, err := newFaceEngine(cfg)
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

// newFaceEngine loads the face models and the ONNX Runtime library: --onnxruntime-lib, else onnxruntime.dll next to mam.exe
// on Windows (the installer puts it there), else the system's standard library search.
func newFaceEngine(cfg config.Config) (*facesim.ONNX, error) {
	exe, err := executablePath()
	if err != nil {
		exe = "" // only the Windows lookup needs it, and RuntimeLibraryPath says so
	}
	lib, err := facesim.RuntimeLibraryPath(runtime.GOOS, cfg.ONNXRuntimeLib, exe)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", facesim.ErrUnavailable, err)
	}
	return facesim.NewONNX(facesim.ONNXConfig{ModelDir: cfg.FaceModels(), RuntimeLibrary: lib})
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
