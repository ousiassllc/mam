package audiodirector

import (
	"context"
	"fmt"
	"math"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Prices are the unit prices (USD) the Audio Director estimates its provider calls with. They are
// estimates for budget control (the vendors bill by credits and plans); Lip Sync prices are the
// per-second list prices of Sync Labs at 25 fps.
type Prices struct {
	// TTSPer1000Chars is ElevenLabs TTS (elevenlabs-tts), per 1000 characters.
	TTSPer1000Chars float64
	// MusicPerSecond is ElevenLabs Music (elevenlabs-music), per generated second.
	MusicPerSecond float64
	// SFXPerSecond is ElevenLabs Sound Effects (elevenlabs-sfx), per generated second.
	SFXPerSecond float64
	// LipSyncPerSecond is Sync Labs (sync-labs) by model, per second of video. A model that is not
	// listed is priced as LipSyncFallback.
	LipSyncPerSecond map[string]float64
	LipSyncFallback  float64
}

// DefaultPrices is the price list used unless the caller changes it.
func DefaultPrices() Prices {
	return Prices{
		TTSPer1000Chars: 0.30,
		MusicPerSecond:  0.02,
		SFXPerSecond:    0.01,
		LipSyncPerSecond: map[string]float64{
			"lipsync-1.9.0-beta": 0.02,
			"lipsync-2":          0.04,
			"sync-3":             0.133,
		},
		LipSyncFallback: 0.133,
	}
}

// TTS is the estimated cost of speaking the text.
func (p Prices) TTS(text string) float64 {
	return float64(len([]rune(text))) / 1000 * p.TTSPer1000Chars
}

// Music is the estimated cost of generating the seconds of music.
func (p Prices) Music(seconds float64) float64 { return seconds * p.MusicPerSecond }

// SFX is the estimated cost of generating the seconds of sound.
func (p Prices) SFX(seconds float64) float64 { return seconds * p.SFXPerSecond }

// LipSync is the estimated cost of lip-syncing the seconds of video with the model.
func (p Prices) LipSync(model string, seconds float64) float64 {
	price, ok := p.LipSyncPerSecond[model]
	if !ok {
		price = p.LipSyncFallback
	}
	return seconds * price
}

// Ledger is the cost ledger (cost_ledger_entry) and the budget of the Project: what Audio
// Production spends is recorded, and the budget ceiling limits it (F-22).
type Ledger interface {
	// ProjectBudget returns the Project budget ceiling in USD (nil: none) and what the ledger holds
	// plus what is reserved.
	ProjectBudget(ctx context.Context, projectID string) (ceiling *float64, spent float64, err error)
	// Reserve holds cost for the Project until release is called (safe to call more than once) and
	// reports ok=false when it does not fit under the ceiling next to the ledger and the other
	// reservations. The check and the hold are one step: of several runs of the Project that spend
	// at the same time, only those that fit under the ceiling go on.
	Reserve(ctx context.Context, projectID string, cost float64) (release func(), ok bool, err error)
	// AddCost appends an entry.
	AddCost(ctx context.Context, e domain.CostEntry) error
}

// BudgetError means the Project budget ceiling does not allow the speech the script needs.
type BudgetError struct {
	Needed, Remaining float64
}

func (e *BudgetError) Error() string {
	return fmt.Sprintf("audiodirector: the speech costs about $%.2f but $%.2f of the budget is left", e.Needed, e.Remaining)
}

// spending tracks what one run spends against the budget ceiling. A provider call is paid for only
// after its estimate is reserved in the Ledger, so runs that go on at the same time cannot together
// pass the ceiling (F-22).
type spending struct {
	ledger    Ledger
	projectID string
	estimated float64 // what the run recorded
}

// reserve holds the cost under the Project budget ceiling until release is called. It returns a
// *BudgetError when the cost does not fit. Release only after the cost is recorded (or the call
// ended without one).
func (s *spending) reserve(ctx context.Context, cost float64) (release func(), err error) {
	if s.ledger == nil {
		return func() {}, nil
	}
	release, ok, err := s.ledger.Reserve(ctx, s.projectID, cost)
	if err != nil {
		return nil, fmt.Errorf("reserve budget: %w", err)
	}
	if ok {
		return release, nil
	}
	ceiling, spent, err := s.ledger.ProjectBudget(ctx, s.projectID)
	if err != nil {
		return nil, fmt.Errorf("read budget: %w", err)
	}
	left := math.Inf(1)
	if ceiling != nil {
		left = max(*ceiling-spent, 0)
	}
	return nil, &BudgetError{Needed: cost, Remaining: left}
}

// record writes the cost of a finished call to the ledger. The provider has already charged, so the
// write does not depend on ctx: a client that went away must not leave the spend uncounted.
func (s *spending) record(ctx context.Context, model domain.ModelName, cost float64) error {
	s.estimated += cost
	if s.ledger == nil {
		return nil
	}
	if err := s.ledger.AddCost(context.WithoutCancel(ctx), domain.CostEntry{ProjectID: s.projectID, Provider: model, EstimatedCost: cost}); err != nil {
		return fmt.Errorf("record cost: %w", err)
	}
	return nil
}
