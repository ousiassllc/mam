package audiodirector_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// A missing ElevenLabs key is known without a call: Produce refuses before the Planner's paid model call.
func TestDirector_ALineWithoutAnElevenLabsKeyIsRefusedBeforeThePlannerRuns(t *testing.T) {
	model := &fakePlanner{answer: `{"scenes":[]}`}
	el := audiodirector.NewElevenLabs(audiodirector.Config{BaseURL: "http://127.0.0.1:1"}, keys(t, false), nil)
	ledger := &fakeLedger{}
	d := audiodirector.New(audiodirector.Deps{
		Providers: audiodirector.ElevenLabsProviders(el), Planner: audiodirector.NewPlanner(model), Ledger: ledger, Tools: tools(t), Root: t.TempDir(),
	})

	_, err := d.Produce(context.Background(), audiodirector.Input{
		ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityFast, Scenes: twoScenes(), Seconds: 10,
	})

	assert.ErrorIs(t, err, audiodirector.ErrNoAPIKey)
	assert.Zero(t, model.calls, "the Planner (a paid model call) is not called")
	assert.Empty(t, ledger.entries)
}

func TestPlanner_EffectShorterThanTheFloorIsDroppedNotStretched(t *testing.T) {
	fake := &fakePlanner{answer: `{"scenes":[{"index":1,"effects":[
		{"layer":"se","at_seconds":1,"duration_seconds":0.3,"prompt":"too short"},
		{"layer":"se","at_seconds":2,"duration_seconds":0,"prompt":"no length"},
		{"layer":"se","at_seconds":3,"duration_seconds":0.5,"prompt":"exactly the floor"}]}]}`}

	plan, err := audiodirector.NewPlanner(fake).Plan(context.Background(), audiodirector.PlanInput{Scenes: []audiodirector.Scene{{Index: 1, Start: 0, End: 10, Clips: 4}}, Scope: domain.AudioScopeOf(domain.QualityBalanced)})

	require.NoError(t, err)
	require.Len(t, plan[1].Effects, 1)
	assert.Equal(t, "exactly the floor", plan[1].Effects[0].Prompt)
	assert.InDelta(t, audiodirector.MinEffectSeconds, plan[1].Effects[0].Seconds, 1e-9)
}

// gatedVendors holds the first music call until the test lets it go, so that a run can be stopped
// with its estimate reserved and not yet in the ledger.
type gatedVendors struct {
	*fakeVendors
	entered chan struct{}
	proceed chan struct{}
	first   atomic.Bool
}

func (g *gatedVendors) Compose(ctx context.Context, prompt string, seconds float64) (audiodirector.Speech, error) {
	if g.first.CompareAndSwap(false, true) { // later calls pass
		close(g.entered)
		<-g.proceed
	}
	return g.fakeVendors.Compose(ctx, prompt, seconds)
}

// gatedSpeech holds the first speech call.
type gatedSpeech struct {
	*fakeVendors
	entered chan struct{}
	proceed chan struct{}
	first   atomic.Bool
}

func (g *gatedSpeech) Synthesize(ctx context.Context, line domain.VoiceLine) (audiodirector.Speech, error) {
	if g.first.CompareAndSwap(false, true) { // later calls pass
		close(g.entered)
		<-g.proceed
	}
	return g.fakeVendors.Synthesize(ctx, line)
}

func TestDirector_RunsOfOneProjectCannotTogetherPassTheCeilingWithTheirSpeech(t *testing.T) {
	prices := audiodirector.DefaultPrices()
	speech := prices.TTS("こんにちは") + prices.TTS("夕暮れ")
	ceiling := speech * 1.5 // one run's speech fits, two do not
	v := &fakeVendors{t: t}
	gate := &gatedSpeech{fakeVendors: v, entered: make(chan struct{}), proceed: make(chan struct{})}
	ledger := &fakeLedger{ceiling: &ceiling}
	d := audiodirector.New(audiodirector.Deps{
		Providers: audiodirector.Providers{Speech: gate, Music: v, Sounds: v, LipSync: v}, Ledger: ledger, Tools: tools(t), Root: t.TempDir(),
	})
	input := func(editTimelineID string) audiodirector.Input {
		return audiodirector.Input{ProjectID: "p1", EditTimelineID: editTimelineID, Quality: domain.QualityFast, Scenes: twoScenes(), Seconds: 10}
	}

	first := make(chan error, 1)
	go func() {
		_, err := d.Produce(context.Background(), input("e1"))
		first <- err
	}()
	<-gate.entered // the first run is in its first TTS call: nothing is in the ledger yet

	_, err := d.Produce(context.Background(), input("e2"))
	close(gate.proceed)
	require.NoError(t, <-first)

	var be *audiodirector.BudgetError
	require.True(t, errors.As(err, &be), "the second run is refused: the first one holds the budget")
	assert.LessOrEqual(t, ledger.spent, ceiling, "the ledger never passes the ceiling")
	assert.Zero(t, ledger.reserved, "every reservation is released")
}

func TestDirector_RunsOfOneProjectCannotTogetherPassTheCeilingWithTheirBGM(t *testing.T) {
	prices := audiodirector.DefaultPrices()
	speech := prices.TTS("こんにちは") + prices.TTS("夕暮れ")
	ceiling := 2*speech + prices.Music(4) + 0.001 // both runs' speech fits, but only one BGM
	v := &fakeVendors{t: t}
	gate := &gatedVendors{fakeVendors: v, entered: make(chan struct{}), proceed: make(chan struct{})}
	ledger := &fakeLedger{ceiling: &ceiling}
	d := audiodirector.New(audiodirector.Deps{
		Providers: audiodirector.Providers{Speech: v, Music: gate, Sounds: v, LipSync: v}, Ledger: ledger, Tools: tools(t), Root: t.TempDir(),
	})
	input := func(editTimelineID string) audiodirector.Input {
		return audiodirector.Input{ProjectID: "p1", EditTimelineID: editTimelineID, Quality: domain.QualityBalanced, Scenes: twoScenes(), Seconds: 10}
	}

	first := make(chan error, 1)
	go func() {
		_, err := d.Produce(context.Background(), input("e1"))
		first <- err
	}()
	<-gate.entered // the first run is in its BGM call: the BGM is reserved, not yet in the ledger

	got, err := d.Produce(context.Background(), input("e2"))
	close(gate.proceed)
	require.NoError(t, err)
	require.NoError(t, <-first)

	assert.Contains(t, skippedLayers(got, domain.SkipBudget), domain.LayerBGM, "the second run's BGM is left out: the first one holds the budget")
	assert.LessOrEqual(t, ledger.spent, ceiling, "the ledger never passes the ceiling")
	assert.Zero(t, ledger.reserved, "every reservation is released")
}
