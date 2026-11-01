package audiodirector

import (
	"context"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// speaker is who speaks the lines of one run, the model its lines are recorded under in the ledger and on the clip, and what
// a line costs (the local voice costs nothing to call, and is still recorded).
type speaker struct {
	model domain.ModelName
	price func(text string) float64
	say   func(ctx context.Context, line domain.VoiceLine) (Speech, error)
}

// speakerFor chooses who speaks the lines of the run, and makes sure they can be spoken before anything is paid for: for ElevenLabs a
// stored API key; for the local voice a reference voice for every speaker (never another voice for one without) and a server that runs.
func (d *Director) speakerFor(ctx context.Context, in Input, lines []domain.VoiceLine) (speaker, error) {
	switch in.VoiceProvider {
	case "", localvoice.ElevenLabs:
		if len(lines) > 0 && d.Providers.Speech == nil {
			return speaker{}, ErrNoAPIKey
		}
		// A missing key is known without a call: it is refused before the Planner's paid model call.
		if kc, ok := d.Providers.Speech.(KeyChecker); ok && len(lines) > 0 {
			if err := kc.CheckKey(); err != nil {
				return speaker{}, err
			}
		}
		return speaker{domain.ModelElevenLabsTTS, d.Prices.TTS, func(ctx context.Context, l domain.VoiceLine) (Speech, error) {
			return d.Providers.Speech.Synthesize(ctx, l)
		}}, nil
	case localvoice.Local:
		local := d.Providers.LocalSpeech
		if err := localvoice.Check(lines, in.LocalVoices, local != nil, func() error { return local.Prepare(ctx) }); err != nil {
			return speaker{}, err
		}
		return speaker{domain.ModelGPTSoVITSTTS, func(string) float64 { return 0 }, func(ctx context.Context, l domain.VoiceLine) (Speech, error) {
			return local.Synthesize(ctx, l, in.LocalVoices[localvoice.SpeakerOf(l)])
		}}, nil
	}
	return speaker{}, fmt.Errorf("%w: %q", localvoice.ErrUnknownProvider, in.VoiceProvider)
}
