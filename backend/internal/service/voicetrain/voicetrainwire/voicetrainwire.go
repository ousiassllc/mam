// Package voicetrainwire builds the fine-tuning of Character voices (package voicetrain) from the services of the app, and gives the local runtime
// manager the one thing the server of GPT-SoVITS needs from it: the directory of the trained voices. It is a package of its own because package app is
// at its line budget.
package voicetrainwire

import (
	"context"
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/localspeech/gpufree"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
	"github.com/ousiassllc/mam/backend/internal/service/voicetrain"
)

// Env is the environment the server of the runtime is started with besides the catalog's (localruntime.Deps.Env): for GPT-SoVITS the one directory
// whose subdirectories it may load trained voices from (MAM_VOICE_WEIGHTS_DIR; the launcher refuses any other path). root is
// voicerefsvc.Service.WeightsRoot, asked at every start (the services are built after the runtime manager).
func Env(root func() string) func(id string) []string {
	return func(id string) []string {
		if id != voicetrain.RuntimeID || root() == "" {
			return nil
		}
		return []string{"MAM_VOICE_WEIGHTS_DIR=" + root()}
	}
}

// bundledComfyUI is the bundled ComfyUI as the guard of the GPU sees it: its address while the process runs ("" otherwise).
type bundledComfyUI struct{ mgr *modelmanager.Service }

func (b bundledComfyUI) URL(ctx context.Context) string {
	if b.mgr == nil || b.mgr.Engine().Process.State != proc.StateRunning {
		return ""
	}
	st, err := b.mgr.Status(ctx)
	if err != nil {
		return ""
	}
	for _, ep := range st.Endpoints {
		if ep.Bundled {
			return ep.URL
		}
	}
	return ""
}

// New is the training service over the local runtime, the voices of the Characters and ffprobe (tools may be nil: then no clip is accepted, the length
// cannot be known). A training never runs together with the bundled ComfyUI: an idle one is asked to let go of its models, a generating one refuses it.
func New(rts *localruntime.Service, mgr *modelmanager.Service, voices *voicerefsvc.Service, tools *media.Tools, assetsDir string, logger *slog.Logger) *voicetrain.Service {
	d := voicetrain.Deps{
		Voices: voices, Runtime: voicetrain.OverRuntime(rts), GPU: gpufree.Guard{Engine: bundledComfyUI{mgr}}, AssetsDir: assetsDir, Logger: logger,
	}
	if tools != nil { // a nil *media.Tools in the interface would not be nil
		d.Probe = tools
	}
	return voicetrain.New(d)
}
