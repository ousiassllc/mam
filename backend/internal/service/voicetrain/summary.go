package voicetrain

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
)

// Summary is what the screen and the MCP tools show of a Character before and around a training: the data it has, whether a training can start
// with it (Problems says why not, in words), the epochs it would run, the trained voice it has and the training of this process (Current).
// There is no time to give before the training runs: nobody has measured one on a PC (spec §16.3), so the time left is told as soon as the first
// epoch of a training stage is done (Progress.eta) and not before.
type Summary struct {
	Clips        []Clip
	DataSeconds  float64
	Problems     []string
	Defaults     Options
	MaxEpochs    Options
	Limits       Limits
	Trained      *voiceref.Finetune
	Current      *Status
	GPUBusy      bool // another Character's training holds the GPU
	HasReference bool
}

// Limits are the amounts of data a training takes, for the screen's words.
type Limits struct {
	MinClipSeconds, MaxClipSeconds, MinTotalSeconds, MaxTotalSeconds, MaxClips int
}

// Summarize collects the summary of the Character: ErrNotFound for an unknown Project, Series or Character.
func (s *Service) Summarize(ctx context.Context, o voicerefsvc.Owner, characterID string) (Summary, error) {
	ref, err := s.Voices.Reference(ctx, o, characterID)
	if err != nil {
		return Summary{}, err
	}
	clips, err := s.Clips(ctx, o, characterID)
	if err != nil {
		return Summary{}, err
	}
	sum := Summary{
		Clips: clips, DataSeconds: seconds(clips), Problems: problems(clips), HasReference: ref != nil,
		Defaults:  Options{EpochsSoVITS: DefaultEpochsSoVITS, EpochsGPT: DefaultEpochsGPT},
		MaxEpochs: Options{EpochsSoVITS: MaxEpochsSoVITS, EpochsGPT: MaxEpochsGPT},
		Limits:    Limits{MinClipSeconds, MaxClipSeconds, MinTotalSeconds, MaxTotalSeconds, MaxClips},
		GPUBusy:   s.Busy(),
	}
	if ref != nil && ref.Finetune != nil {
		sum.Trained = ref.Finetune
	}
	if st, ok := s.Current(o, characterID); ok {
		sum.Current = &st
	}
	return sum, nil
}
