package voicetrain

import (
	"context"
	"errors"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

// gpuRefusal is the refusal of a training while the bundled ComfyUI holds the GPU (gpufree.Guard.Before): its sentence is kept, with the common
// remedy of a conflict. An error of another kind is returned as it is.
func gpuRefusal(err error) error {
	var un *localvoice.UnavailableError
	if errors.As(err, &un) {
		return conflictf("同梱の ComfyUI が生成中で、GPU が空いていません（学習は ComfyUI と同時には動かしません）。生成が終わってから、学習を始めてください")
	}
	return err
}

// runtimeRefusal is the refusal of the runtime: a machine that cannot run it or is not set up for it says why in its own words (localruntime), a
// training that already holds it is told as such.
func runtimeRefusal(err error) error {
	var support *localruntime.SupportError
	var weights *localruntime.WeightsError
	switch {
	case errors.As(err, &support):
		return conflictf("%s", support.Error())
	case errors.Is(err, localruntime.ErrNotInstalled):
		return conflictf("GPT-SoVITS が導入されていません。モデルマネージャーの「ローカル音声・口パク」から導入してください")
	case errors.As(err, &weights):
		return conflictf("%s。モデルマネージャーの「ローカル音声・口パク」で GPT-SoVITS の「重みを導入」を実行してください（学習には、声を作るだけのときには要らない重みも使います）", weights.Error())
	case errors.Is(err, localruntime.ErrBusy):
		return conflictf("GPT-SoVITS の導入が実行中です。終わってから、または中断してから、学習を始めてください")
	case errors.Is(err, localruntime.ErrTraining):
		return conflictf("すでに学習が動いています")
	}
	return err
}

// OverRuntime is the Runtime of the local runtime service.
func OverRuntime(s *localruntime.Service) Runtime { return overRuntime{s} }

type overRuntime struct{ s *localruntime.Service }

func (o overRuntime) BeginTool(ctx context.Context, id string, assets ...string) (Held, error) {
	t, err := o.s.BeginTool(ctx, id, assets...)
	if err != nil {
		return nil, err
	}
	return held{t}, nil
}

type held struct{ t *localruntime.Tool }

func (h held) Spec(script string, args, env []string) (proc.Spec, error) {
	return h.t.Spec(script, args, env)
}
func (h held) WorkDir() string    { return h.t.Dir }
func (h held) GPU() (string, int) { return h.t.GPUName, h.t.VRAMGB }
func (h held) End()               { h.t.End() }
