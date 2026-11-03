// Package audiowire builds the vendors of the Audio Director for the app. It is a package of its own, under app, so that app stays within its size
// (linterly).
package audiowire

import (
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/gptsovits"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/musetalk"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/synclabs"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/service/locallipsync"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/localspeech"
	"github.com/ousiassllc/mam/backend/internal/service/localspeech/irodoriwire"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// Providers are the vendors of the Audio Director: ElevenLabs (TTS, cloned voices, Music, Sound Effects; API key "elevenlabs"),
// Sync Labs (Lip Sync; API key "synclabs") and the local voices and lip sync, GPT-SoVITS, Irodori-TTS and MuseTalk 1.5 of the local runtime
// manager, which a run may choose instead of ElevenLabs' TTS and Sync Labs. A runtime need not be installed: the local provider reports that when
// a run chooses it. The lip sync is told the ffmpeg of tools, the one the Director runs (nil: none; the Director is not available then either).
func Providers(keys credential.Store, rts *localruntime.Service, mgr *modelmanager.Service, tools *media.Tools) audiodirector.Providers {
	p := audiodirector.ElevenLabsProviders(audiodirector.NewElevenLabs(audiodirector.Config{}, keys, nil))
	p.LipSync = synclabs.New(synclabs.Config{}, keys, nil)
	p.LocalSpeech = gptsovits.New(localspeech.Server{Runtime: rts}, gptsovits.Config{}, nil)
	p.LocalIrodori = irodoriwire.Voice(rts, mgr)
	lip := musetalk.Config{}
	if tools != nil {
		lip.FFmpegPath = tools.FFmpegPath()
	}
	p.LocalLipSync = musetalk.New(locallipsync.Runner{Runtime: rts}, lip)
	return p
}
