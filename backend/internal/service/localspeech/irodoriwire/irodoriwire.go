// Package irodoriwire builds the local voice Irodori-TTS of the Audio Director from the services of the app: the server of the runtime manager, which
// the voice makes room for on the GPU before it starts. It is a package of its own because package app is at its line budget.
package irodoriwire

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/irodori"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/localspeech"
	"github.com/ousiassllc/mam/backend/internal/service/localspeech/gpufree"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

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

// Guard is the guard of the GPU for the bundled ComfyUI of mgr: the voice and the local LLM (package llamaserver) make room with it before their server starts.
func Guard(mgr *modelmanager.Service) gpufree.Guard {
	return gpufree.Guard{Engine: bundledComfyUI{mgr}}
}

// Voice is the local voice Irodori-TTS of the Audio Director: the server of the runtime manager, which the voice makes room for on the
// GPU before it starts (it never runs at the same moment as the bundled ComfyUI: spec/architecture/model-adapters-m3.md §14.12).
func Voice(rts *localruntime.Service, mgr *modelmanager.Service) audiodirector.LocalSynthesizer {
	return irodori.New(localspeech.IrodoriServer(rts, Guard(mgr).Before), irodori.Config{}, nil)
}
