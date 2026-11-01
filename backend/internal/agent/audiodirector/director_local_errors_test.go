package audiodirector_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
)

// Every kind of the typed error of a local voice that cannot be used reaches the caller as it is, from the start of the runtime (before
// any payment) and from a line.
func TestDirector_LocalAnUnusableRuntimeIsTheTypedErrorOfItsKind(t *testing.T) {
	voices := map[string]localvoice.Voice{"美咲": yui, voiceref.NarrationName: narrator}
	for _, kind := range []localvoice.Kind{
		localvoice.KindNotInstalled, localvoice.KindWeightsMissing, localvoice.KindUnsupported,
		localvoice.KindGPUMemory, localvoice.KindProcess, localvoice.KindTimeout,
	} {
		t.Run(string(kind)+" at the start", func(t *testing.T) {
			cloud := &fakeVendors{t: t}
			local := &fakeLocal{t: t, prepare: &localvoice.UnavailableError{Kind: kind, Detail: "reason"}}
			ledger := &fakeLedger{}
			d, _ := newLocalDirector(t, cloud, local, ledger)

			_, err := d.Produce(context.Background(), localInput(voices))

			var un *localvoice.UnavailableError
			require.ErrorAs(t, err, &un)
			assert.Equal(t, kind, un.Kind)
			assert.Equal(t, "reason", un.Detail)
			assert.Empty(t, local.lines, "no line is spoken")
			assert.Empty(t, ledger.entries)
			assert.Zero(t, ledger.reserved)
		})
		t.Run(string(kind)+" at a line", func(t *testing.T) {
			cloud := &fakeVendors{t: t}
			local := &fakeLocal{t: t, fail: &localvoice.UnavailableError{Kind: kind}}
			d, _ := newLocalDirector(t, cloud, local, &fakeLedger{})

			_, err := d.Produce(context.Background(), localInput(voices))

			var un *localvoice.UnavailableError
			require.ErrorAs(t, err, &un)
			assert.Equal(t, kind, un.Kind)
		})
	}
}

func TestDirector_LocalWithoutTheProviderIsKindNotInstalled(t *testing.T) {
	d := audiodirector.New(audiodirector.Deps{Providers: (&fakeVendors{t: t}).providers(), Tools: tools(t), Root: t.TempDir()})

	_, err := d.Produce(context.Background(), localInput(map[string]localvoice.Voice{"美咲": yui, voiceref.NarrationName: narrator}))

	var un *localvoice.UnavailableError
	require.ErrorAs(t, err, &un)
	assert.Equal(t, localvoice.KindNotInstalled, un.Kind)
}

func TestDirector_LocalAScriptWithoutLinesNeedsNeitherTheVoicesNorTheRuntime(t *testing.T) {
	cloud, local := &fakeVendors{t: t}, &fakeLocal{t: t, prepare: &localvoice.UnavailableError{Kind: localvoice.KindNotInstalled}}
	d, _ := newLocalDirector(t, cloud, local, nil)
	in := localInput(nil)
	in.Scenes = []audiodirector.Scene{{Index: 1, Start: 0, End: 5, Clips: 2}}

	got, err := d.Produce(context.Background(), in)

	require.NoError(t, err)
	assert.Zero(t, local.prepared)
	assert.Zero(t, got.Count(domain.LayerDialogue))
}

func TestDirector_AnUnknownVoiceProviderIsRefusedBeforeAnythingIsPaid(t *testing.T) {
	cloud, local := &fakeVendors{t: t}, &fakeLocal{t: t}
	d, _ := newLocalDirector(t, cloud, local, &fakeLedger{})
	in := localInput(nil)
	in.VoiceProvider = "azure"

	_, err := d.Produce(context.Background(), in)

	require.ErrorIs(t, err, localvoice.ErrUnknownProvider)
	assert.Empty(t, cloud.lines)
	assert.Empty(t, local.lines)
}

// The choice defaults to ElevenLabs, and a run that names it behaves as before: its cloned voices, its price, its model.
func TestDirector_ElevenLabsIsTheDefaultAndKeepsItsVoicesPriceAndModel(t *testing.T) {
	for name, provider := range map[string]localvoice.Provider{"unset": "", "named": localvoice.ElevenLabs} {
		t.Run(name, func(t *testing.T) {
			cloud, local := &fakeVendors{t: t}, &fakeLocal{t: t}
			ledger := &fakeLedger{}
			d, _ := newLocalDirector(t, cloud, local, ledger)
			in := localInput(map[string]localvoice.Voice{"美咲": yui, voiceref.NarrationName: narrator})
			in.VoiceProvider = provider

			got, err := d.Produce(context.Background(), in)

			require.NoError(t, err)
			assert.Empty(t, local.lines)
			assert.Zero(t, local.prepared, "the local runtime is not started for a cloud run")
			require.Len(t, cloud.lines, 2)
			assert.Equal(t, "cloud-voice", cloud.lines[0].VoiceID, "the cloned voice still speaks the speaker's lines")
			for _, c := range got.Clips {
				if c.LayerOf() == domain.LayerDialogue || c.LayerOf() == domain.LayerNarration {
					assert.Equal(t, domain.ModelElevenLabsTTS, c.Model)
				}
			}
			var cost float64
			for _, e := range ledger.entries {
				if e.Provider == domain.ModelElevenLabsTTS {
					cost += e.EstimatedCost
				}
			}
			assert.InDelta(t, audiodirector.DefaultPrices().TTS("こんにちは")+audiodirector.DefaultPrices().TTS("夕暮れ"), cost, 1e-9)
		})
	}
}

// The missing key of ElevenLabs does not matter to a run that is spoken by the local voice, and the other way round.
func TestDirector_TheChosenProviderDecidesWhichPrerequisiteIsChecked(t *testing.T) {
	t.Run("a local run does not need the ElevenLabs key", func(t *testing.T) {
		local := &fakeLocal{t: t}
		root := t.TempDir()
		el := audiodirector.NewElevenLabs(audiodirector.Config{BaseURL: "http://127.0.0.1:1"}, keys(t, false), nil)
		d := audiodirector.New(audiodirector.Deps{Providers: audiodirector.Providers{Speech: el, LocalSpeech: local}, Tools: tools(t), Root: root})

		_, err := d.Produce(context.Background(), localInput(map[string]localvoice.Voice{"美咲": yui, voiceref.NarrationName: narrator}))

		require.NoError(t, err)
		assert.Len(t, local.lines, 2)
	})
	t.Run("an ElevenLabs run still needs it", func(t *testing.T) {
		local := &fakeLocal{t: t}
		el := audiodirector.NewElevenLabs(audiodirector.Config{BaseURL: "http://127.0.0.1:1"}, keys(t, false), nil)
		d := audiodirector.New(audiodirector.Deps{Providers: audiodirector.Providers{Speech: el, LocalSpeech: local}, Tools: tools(t), Root: t.TempDir()})
		in := localInput(nil)
		in.VoiceProvider = ""

		_, err := d.Produce(context.Background(), in)

		assert.ErrorIs(t, err, audiodirector.ErrNoAPIKey)
	})
}

func TestDirector_LocalAFailedLineRemovesTheRunAndKeepsWhatWasRecorded(t *testing.T) {
	cloud, local := &fakeVendors{t: t}, &fakeLocal{t: t, fail: &audiodirector.ProviderError{Vendor: localvoice.Vendor, Status: 400, Message: "tts failed"}}
	ledger := &fakeLedger{}
	d, root := newLocalDirector(t, cloud, local, ledger)

	_, err := d.Produce(context.Background(), localInput(map[string]localvoice.Voice{"美咲": yui, voiceref.NarrationName: narrator}))

	var pe *audiodirector.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, localvoice.Vendor, pe.Vendor)
	assert.Empty(t, ledger.entries, "a line that failed is not recorded")
	_, statErr := os.Stat(filepath.Join(root, "p1", "audio", "e1"))
	assert.True(t, os.IsNotExist(statErr) || dirEmpty(t, filepath.Join(root, "p1", "audio", "e1")))
}

func dirEmpty(t *testing.T, dir string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	return len(entries) == 0
}
