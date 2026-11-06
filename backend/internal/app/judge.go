package app

import (
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/agent/judge"
	"github.com/ousiassllc/mam/backend/internal/app/facewire"
	"github.com/ousiassllc/mam/backend/internal/config"
	"github.com/ousiassllc/mam/backend/internal/facesim"
	"github.com/ousiassllc/mam/backend/internal/facesim/holder"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/service/ffgate"
)

// judgeSetup is what the AI Video Judge needs from the machine: ffmpeg to sample frames and, for
// the Face Identity Similarity, the local face models.
type judgeSetup struct {
	config judge.Config
	// frames samples the Takes through the ffmpeg of the app; the Judge is off (ffgate.Frames is not Available) while ffmpeg is missing.
	frames judge.FrameSource
	// faces is the face engine. It is always there, and loads the models when they are first used: it is off (Off says why, #1026) while the models or
	// the ONNX Runtime library are missing, and Takes are then judged qualitatively only. The models can be installed from the model manager while mam runs.
	faces *holder.Holder
}

// newJudgeSetup loads the Judge setting and looks for the face models (missing ones are not fatal). tools is the ffmpeg of the app: the Judge follows it.
func newJudgeSetup(cfg config.Config, tools *media.Tools, logger *slog.Logger) (judgeSetup, error) {
	s := judgeSetup{}
	var err error
	if s.config, err = judge.LoadConfig(cfg.JudgeConfigPath); err != nil {
		return s, err
	}
	s.frames = ffgate.Frames{FrameSource: judge.NewFFmpeg(*tools), Tools: tools}
	s.faces = facewire.Engine(cfg, executablePath)
	if off := s.faces.Off(); off != nil { // loads the models now when they are there, so that a broken install shows at the start
		logger.Warn("face identity similarity is off: Takes are judged qualitatively only (install the face models from the model manager)", "model_dir", cfg.FaceModels(), "reason", off.Reason, "missing", off.Missing, "detail", off.Detail)
	}
	return s, nil
}

// close releases the face models.
func (s judgeSetup) close() { _ = s.faces.Close() }

// engine is the face engine (a Holder: Usable says whether it works now).
func (s judgeSetup) engine() facesim.Engine { return s.faces }
