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
	// ProjectBudget returns the Project budget ceiling in USD (nil: none) and what the ledger holds.
	ProjectBudget(ctx context.Context, projectID string) (ceiling *float64, spent float64, err error)
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

// spending tracks what one run spends against the budget ceiling.
type spending struct {
	ledger    Ledger
	projectID string
	ceiling   *float64
	spent     float64 // in the ledger before the run plus what the run recorded
	estimated float64 // what the run recorded
}

func newSpending(ctx context.Context, ledger Ledger, projectID string) (*spending, error) {
	s := &spending{ledger: ledger, projectID: projectID}
	if ledger == nil {
		return s, nil
	}
	ceiling, spent, err := ledger.ProjectBudget(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("read budget: %w", err)
	}
	s.ceiling, s.spent = ceiling, spent
	return s, nil
}

// remaining is what the budget still allows (+Inf without a ceiling).
func (s *spending) remaining() float64 {
	if s.ceiling == nil {
		return math.Inf(1)
	}
	return max(*s.ceiling-s.spent, 0)
}

// fits reports whether the cost keeps the Project within its budget ceiling.
func (s *spending) fits(cost float64) bool { return cost <= s.remaining() }

// record writes the cost of a finished call to the ledger. The provider has already charged, so the
// write does not depend on ctx: a client that went away must not leave the spend uncounted.
func (s *spending) record(ctx context.Context, model domain.ModelName, cost float64) error {
	s.spent += cost
	s.estimated += cost
	if s.ledger == nil {
		return nil
	}
	if err := s.ledger.AddCost(context.WithoutCancel(ctx), domain.CostEntry{ProjectID: s.projectID, Provider: model, EstimatedCost: cost}); err != nil {
		return fmt.Errorf("record cost: %w", err)
	}
	return nil
}
