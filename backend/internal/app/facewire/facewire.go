// Package facewire builds the face engine and the install of the face check models for the app. It is a package of its own, under app, so that app stays within its size (linterly).
package facewire

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"

	"github.com/ousiassllc/mam/backend/internal/config"
	"github.com/ousiassllc/mam/backend/internal/facesim"
	"github.com/ousiassllc/mam/backend/internal/facesim/holder"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/facemodels"
)

// Engine is the face engine: it loads the face models and the ONNX Runtime library when they are first used (and again after they were installed or removed while
// mam runs, Issue #1026): --onnxruntime-lib, else onnxruntime.dll next to mam.exe on Windows (the installer puts it there), else the system's standard library
// search. executable is the running mam executable (the Windows lookup is the only thing that needs it).
func Engine(cfg config.Config, executable func() (string, error)) *holder.Holder {
	return engineOn(runtime.GOOS, cfg, executable)
}

// engineOn is Engine for the operating system goos (a test takes Windows, whose DLL lookup can fail, on any machine).
func engineOn(goos string, cfg config.Config, executable func() (string, error)) *holder.Holder {
	models := facesim.ONNXConfig{ModelDir: cfg.FaceModels()}
	return holder.New(func() (holder.Loaded, error) {
		exe, err := executable()
		if err != nil {
			exe = "" // only the Windows lookup needs it, and RuntimeLibraryPath says so
		}
		lib, err := facesim.RuntimeLibraryPath(goos, cfg.ONNXRuntimeLib, exe)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", facesim.ErrUnavailable, err)
		}
		engine, err := facesim.NewONNX(facesim.ONNXConfig{ModelDir: models.ModelDir, RuntimeLibrary: lib})
		if err != nil {
			return nil, err
		}
		return engine, nil
	}, models.Diagnose)
}

// Models is the install of the face check models (YuNet, SFace) into the model folder, from the model manager. Its record is below the data directory (the model
// folder may be one the user chose, and the data directory is always writable). nil when it cannot be opened.
func Models(cfg config.Config, engine *holder.Holder, logger *slog.Logger) *facemodels.Service {
	svc, err := facemodels.New(facemodels.Deps{StateDir: filepath.Join(cfg.DataDir, "facemodels"), ModelDir: cfg.FaceModels(), Engine: engine})
	if err != nil {
		logger.Warn("face models: open failed", "err", err)
		return nil
	}
	return svc
}
