package audiodirector_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestDirector_AFailedRunLeavesNoFilesBehindButTheLedgerKeepsTheCost(t *testing.T) {
	v := &failOnLine{fakeVendors: &fakeVendors{t: t}, n: 2}
	ledger := &fakeLedger{}
	root := t.TempDir()
	d := audiodirector.New(audiodirector.Deps{Providers: audiodirector.Providers{Speech: v}, Ledger: ledger, Tools: tools(t), Root: root})

	_, err := d.Produce(context.Background(), audiodirector.Input{
		ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityFast, Scenes: twoScenes(), Seconds: 10, RunID: "run1",
	})

	require.Error(t, err)
	assert.Equal(t, 2, v.calls)
	assert.Len(t, ledger.entries, 1, "the first line was paid for")
	_, statErr := os.Stat(filepath.Join(root, "p1", "audio", "e1", "run1"))
	assert.True(t, os.IsNotExist(statErr), "the run directory is removed")
}

func TestDirector_DiscardRunRemovesTheRunOfASuccessfulProduce(t *testing.T) {
	v := &fakeVendors{t: t}
	d, root := newDirector(t, v, nil, nil)
	in := audiodirector.Input{ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityFast, Scenes: twoScenes(), Seconds: 10, RunID: "run1"}
	got, err := d.Produce(context.Background(), in)
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(root, filepath.FromSlash(got.MixPath)))

	require.NoError(t, d.DiscardRun(in))

	_, statErr := os.Stat(filepath.Join(root, "p1", "audio", "e1", "run1"))
	assert.True(t, os.IsNotExist(statErr))
	assert.ErrorIs(t, d.DiscardRun(audiodirector.Input{ProjectID: "p1", EditTimelineID: "..", RunID: "x"}), audiodirector.ErrUnsafePath)
}

// cancelAfter cancels the run right after the provider answered: the vendor has charged by then.
type cancelAfter struct {
	*fakeVendors
	cancel             context.CancelFunc
	speech, music, sfx bool
	lipSync            bool
}

func (c *cancelAfter) Synthesize(ctx context.Context, line domain.VoiceLine) (audiodirector.Speech, error) {
	sp, err := c.fakeVendors.Synthesize(ctx, line)
	if c.speech {
		c.cancel()
	}
	return sp, err
}

func (c *cancelAfter) Compose(ctx context.Context, prompt string, seconds float64) (audiodirector.Speech, error) {
	sp, err := c.fakeVendors.Compose(ctx, prompt, seconds)
	if c.music {
		c.cancel()
	}
	return sp, err
}

func (c *cancelAfter) Generate(ctx context.Context, prompt string, seconds float64, loop bool) (audiodirector.Speech, error) {
	sp, err := c.fakeVendors.Generate(ctx, prompt, seconds, loop)
	if c.sfx {
		c.cancel()
	}
	return sp, err
}

func (c *cancelAfter) Sync(ctx context.Context, req audiodirector.LipSyncRequest) ([]byte, error) {
	data, err := c.fakeVendors.Sync(ctx, req)
	if c.lipSync {
		c.cancel()
	}
	return data, err
}

// liveLedger refuses a write with a cancelled context, as the database does.
type liveLedger struct{ fakeLedger }

func (l *liveLedger) AddCost(ctx context.Context, e domain.CostEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return l.fakeLedger.AddCost(ctx, e)
}

func TestDirector_ACostTheProviderChargedIsRecordedWhenTheRunIsCancelledAfterTheCall(t *testing.T) {
	tests := []struct {
		name    string
		quality domain.QualityPolicy
		set     func(*cancelAfter)
		want    domain.ModelName // the model of the call that was cancelled right after it was paid
	}{
		{"speech", domain.QualityFast, func(c *cancelAfter) { c.speech = true }, domain.ModelElevenLabsTTS},
		{"music", domain.QualityFast, func(c *cancelAfter) { c.music = true }, domain.ModelElevenLabsMusic},
		{"sound effects", domain.QualityBalanced, func(c *cancelAfter) { c.sfx = true }, domain.ModelElevenLabsSFX},
		{"lip sync", domain.QualityMaxQuality, func(c *cancelAfter) { c.lipSync = true }, domain.ModelSyncLabs},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			v := &cancelAfter{fakeVendors: &fakeVendors{t: t}, cancel: cancel}
			tc.set(v)
			ledger := &liveLedger{}
			d := audiodirector.New(audiodirector.Deps{
				Providers: audiodirector.Providers{Speech: v, Music: v, Sounds: v, LipSync: v}, Ledger: ledger, Tools: tools(t), Root: t.TempDir(),
			})
			take := testVideo(t, 4)
			clips := []audiodirector.VideoClip{{Index: 1, TakeID: "t1", Path: take, In: 0, Out: 4, Start: 0}, {Index: 2, TakeID: "t2", Path: take, In: 0, Out: 4, Start: 4}}

			_, err := d.Produce(ctx, audiodirector.Input{
				ProjectID: "p1", EditTimelineID: "e1", Quality: tc.quality, Scenes: twoScenes(), Clips: clips, Seconds: 10,
			})

			require.ErrorIs(t, err, context.Canceled)
			models := ledger.providers()
			require.NotEmpty(t, models)
			assert.Equal(t, tc.want, models[len(models)-1], "the paid call is the last entry of the ledger")
		})
	}
}

// failingSync fails after Sync Labs accepted the job (or before, when err is not a ChargedError).
type failingSync struct {
	*fakeVendors
	err    error
	cancel context.CancelFunc
	calls  int
}

func (f *failingSync) Sync(context.Context, audiodirector.LipSyncRequest) ([]byte, error) {
	f.calls++
	if f.cancel != nil {
		f.cancel()
	}
	return nil, f.err
}

// A Lip Sync that Sync Labs accepted is billed even when no video came back (download failure,
// timeout, cancelled run): its estimate is in the ledger once, and the clip is listed in Skipped.
func TestDirector_ALipSyncSyncLabsAcceptedButFailedLaterIsRecordedOncePerJob(t *testing.T) {
	charged := &audiodirector.ChargedError{Err: &audiodirector.ProviderError{Vendor: audiodirector.VendorSyncLabs, Status: 404, Message: "download of the output failed"}}
	take := testVideo(t, 4)
	clips := []audiodirector.VideoClip{{Index: 1, TakeID: "t1", Path: take, In: 0, Out: 4, Start: 0}, {Index: 2, TakeID: "t2", Path: take, In: 0, Out: 4, Start: 4}}
	in := audiodirector.Input{ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityMaxQuality, Scenes: twoScenes(), Clips: clips, Seconds: 10}
	// lipSyncEntries counts the Sync Labs entries of the ledger.
	lipSyncEntries := func(l *fakeLedger) (n int) {
		for _, p := range l.providers() {
			if p == domain.ModelSyncLabs {
				n++
			}
		}
		return n
	}

	t.Run("accepted then the output cannot be downloaded", func(t *testing.T) {
		v := &failingSync{fakeVendors: &fakeVendors{t: t}, err: charged}
		ledger := &fakeLedger{}
		d := audiodirector.New(audiodirector.Deps{Providers: v.providersWith(v), Ledger: ledger, Tools: tools(t), Root: t.TempDir()})

		got, err := d.Produce(context.Background(), in)

		require.NoError(t, err)
		assert.Empty(t, got.LipSync)
		assert.Contains(t, skippedLayers(got, domain.SkipError), domain.LayerLipSync)
		assert.Equal(t, v.calls, lipSyncEntries(ledger), "one entry per accepted job")
		assert.Positive(t, v.calls)
		assert.InDelta(t, ledger.spent, got.EstimatedCost, 1e-9, "the cost is part of the estimate")
	})

	t.Run("refused before the job exists", func(t *testing.T) {
		v := &failingSync{fakeVendors: &fakeVendors{t: t}, err: &audiodirector.ProviderError{Vendor: audiodirector.VendorSyncLabs, Status: 400, Message: "no"}}
		ledger := &fakeLedger{}
		d := audiodirector.New(audiodirector.Deps{Providers: v.providersWith(v), Ledger: ledger, Tools: tools(t), Root: t.TempDir()})

		_, err := d.Produce(context.Background(), in)

		require.NoError(t, err)
		assert.Zero(t, lipSyncEntries(ledger))
	})

	t.Run("the run is cancelled while the job runs", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		v := &failingSync{fakeVendors: &fakeVendors{t: t}, err: &audiodirector.ChargedError{Err: context.Canceled}, cancel: cancel}
		ledger := &liveLedger{}
		d := audiodirector.New(audiodirector.Deps{Providers: v.providersWith(v), Ledger: ledger, Tools: tools(t), Root: t.TempDir()})

		_, err := d.Produce(ctx, in)

		require.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, domain.ModelSyncLabs, ledger.providers()[len(ledger.entries)-1])
	})
}
