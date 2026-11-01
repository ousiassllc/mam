package localspeech_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/localspeech"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
)

// runtime is the runtime manager: Prepare answers with err, Status with the address the process listens on.
type runtime struct {
	err       error
	statusErr error
	url       string
	prepared  []string
}

func (r *runtime) Prepare(_ context.Context, id string) error {
	r.prepared = append(r.prepared, id)
	return r.err
}

func (r *runtime) Status(context.Context, string) (localruntime.View, error) {
	return localruntime.View{Process: proc.Status{State: proc.StateRunning, URL: r.url}}, r.statusErr
}

func TestServer_EnsureStartsTheRuntimeAndReturnsItsAddress(t *testing.T) {
	rt := &runtime{url: "http://127.0.0.1:9880"}

	base, err := localspeech.Server{Runtime: rt}.Ensure(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:9880", base)
	assert.Equal(t, []string{"gpt-sovits"}, rt.prepared, "the runtime of the catalog")
}

func TestServer_EnsureWithoutAnAddressIsKindProcess(t *testing.T) {
	_, err := localspeech.Server{Runtime: &runtime{}}.Ensure(context.Background())

	var un *localvoice.UnavailableError
	require.ErrorAs(t, err, &un)
	assert.Equal(t, localvoice.KindProcess, un.Kind)
}

func TestUnavailable_EveryRefusalOfTheRuntimeManagerHasItsKind(t *testing.T) {
	support := func(code rtm.Code) error {
		return &localruntime.SupportError{Runtime: "gpt-sovits", Support: rtm.Support{Code: code, Reason: "この PC では使えません", Fallback: "ElevenLabs を使ってください"}}
	}
	for name, tc := range map[string]struct {
		err    error
		kind   localvoice.Kind
		detail string
	}{
		"another OS (macOS)":       {support(rtm.CodeOS), localvoice.KindUnsupported, "この PC では使えません ElevenLabs を使ってください"},
		"no NVIDIA GPU":            {support(rtm.CodeNotNVIDIA), localvoice.KindUnsupported, "この PC では使えません"},
		"another CPU":              {support(rtm.CodeArch), localvoice.KindUnsupported, ""},
		"no GPU":                   {support(rtm.CodeNoGPU), localvoice.KindUnsupported, ""},
		"too little video memory":  {support(rtm.CodeLowVRAM), localvoice.KindGPUMemory, "この PC では使えません"},
		"not installed":            {localruntime.ErrNotInstalled, localvoice.KindNotInstalled, ""},
		"an install is running":    {localruntime.ErrBusy, localvoice.KindNotInstalled, ""},
		"unknown runtime":          {localruntime.ErrNotFound, localvoice.KindNotInstalled, ""},
		"weights not downloaded":   {&localruntime.WeightsError{Runtime: "gpt-sovits", Name: "GPT-SoVITS", Assets: []rtm.AssetStatus{{Name: "w.bin", State: rtm.AssetMissing}}}, localvoice.KindWeightsMissing, "w.bin"},
		"the start took too long":  {fmt.Errorf("GPT-SoVITS が起動しませんでした: %w", context.DeadlineExceeded), localvoice.KindTimeout, ""},
		"the process did not come": {errors.New("GPT-SoVITS を起動できません: exec failed"), localvoice.KindProcess, "exec failed"},
		"a wrapped refusal":        {fmt.Errorf("start: %w", localruntime.ErrNotInstalled), localvoice.KindNotInstalled, ""},
	} {
		t.Run(name, func(t *testing.T) {
			got := localspeech.Unavailable(context.Background(), tc.err)

			var un *localvoice.UnavailableError
			require.ErrorAs(t, got, &un)
			assert.Equal(t, tc.kind, un.Kind)
			assert.Contains(t, un.Detail, tc.detail)
			assert.ErrorIs(t, got, errors.Unwrap(un), "the manager's error is behind it")
		})
	}
}

func TestUnavailable_TheCancellationOfTheCallerIsNotAFailureOfTheRuntime(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := localspeech.Unavailable(ctx, context.Canceled)

	assert.ErrorIs(t, got, context.Canceled)
	var un *localvoice.UnavailableError
	assert.False(t, errors.As(got, &un))
}

func TestReason_NamesTheSpeakersWithoutAVoiceAndEveryKindOfUnavailable(t *testing.T) {
	got, ok := localspeech.Reason(fmt.Errorf("produce: %w", &localvoice.MissingVoiceError{Speakers: []localvoice.MissingSpeaker{
		{Name: "ユイ", Cause: voicerefsvc.ErrNoReference}, {Name: voiceref.NarrationName, Cause: voicerefsvc.ErrNoCharacter},
	}}))
	require.True(t, ok)
	assert.Contains(t, got, "「ユイ」、「ナレーション」")
	assert.Contains(t, got, "ElevenLabs")

	for _, kind := range []localvoice.Kind{
		localvoice.KindNotInstalled, localvoice.KindWeightsMissing, localvoice.KindUnsupported,
		localvoice.KindGPUMemory, localvoice.KindProcess, localvoice.KindTimeout,
	} {
		text, ok := localspeech.Reason(&localvoice.UnavailableError{Kind: kind, Detail: "理由の詳細"})
		require.True(t, ok, kind)
		assert.Contains(t, text, "GPT-SoVITS", kind)
		assert.Contains(t, text, "理由の詳細", kind)
	}
	notInstalled, _ := localspeech.Reason(&localvoice.UnavailableError{Kind: localvoice.KindNotInstalled})
	weights, _ := localspeech.Reason(&localvoice.UnavailableError{Kind: localvoice.KindWeightsMissing})
	gpu, _ := localspeech.Reason(&localvoice.UnavailableError{Kind: localvoice.KindGPUMemory})
	assert.NotEqual(t, notInstalled, weights)
	assert.NotEqual(t, weights, gpu)

	_, ok = localspeech.Reason(errors.New("boom"))
	assert.False(t, ok)
	_, ok = localspeech.Reason(audiodirector.ErrNoAPIKey)
	assert.False(t, ok, "the errors of ElevenLabs are not the local voice's")
	_, ok = localspeech.Reason(localvoice.ErrUnknownProvider)
	assert.True(t, ok)
}

// lookup is the lookup of the reference voice of a speaker with fixed answers.
type lookup struct {
	voices map[string]voicerefsvc.Voice
	errs   map[string]error
	asked  []string
}

func (l *lookup) ForSpeaker(_ context.Context, project, branch, speaker string) (voicerefsvc.Voice, error) {
	l.asked = append(l.asked, project+"/"+branch+"/"+speaker)
	if err := l.errs[speaker]; err != nil {
		return voicerefsvc.Voice{}, err
	}
	return l.voices[speaker], nil
}

func TestVoices_LooksUpEverySpeakerOnceAndTheNarrationByItsCharacterName(t *testing.T) {
	l := &lookup{
		voices: map[string]voicerefsvc.Voice{
			"ユイ":                   {Path: "/a/yui.wav", Reference: voiceref.Reference{Text: "ユイです", Language: "ja"}},
			voiceref.NarrationName: {Path: "/a/na.wav", Reference: voiceref.Reference{Language: "en"}},
		},
	}
	scenes := []audiodirector.Scene{
		{Lines: []string{"ユイ: こんにちは", "ナレーション: 朝", "  ", "NA: 昼"}},
		{Lines: []string{"ユイ: さようなら", "ただの地の文"}},
	}

	got, err := localspeech.Voices(context.Background(), l, "p1", "b1", scenes)

	require.NoError(t, err)
	assert.Equal(t, map[string]localvoice.Voice{
		"ユイ":                   {Path: "/a/yui.wav", Text: "ユイです", Language: "ja"},
		voiceref.NarrationName: {Path: "/a/na.wav", Language: "en"},
	}, got)
	assert.Equal(t, []string{"p1/b1/ユイ", "p1/b1/ナレーション"}, l.asked, "once per speaker, in the Branch")
}

func TestVoices_ASpeakerWithoutAVoiceIsInTheMapWithTheCause(t *testing.T) {
	for _, cause := range []error{voicerefsvc.ErrNoCharacter, voicerefsvc.ErrNoReference, voicerefsvc.ErrAmbiguous, voicerefsvc.ErrFileMissing} {
		l := &lookup{errs: map[string]error{"ユイ": fmt.Errorf("%w: %q", cause, "ユイ")}}

		got, err := localspeech.Voices(context.Background(), l, "p1", "", []audiodirector.Scene{{Lines: []string{"ユイ: こんにちは"}}})

		require.NoError(t, err)
		require.Contains(t, got, "ユイ")
		assert.False(t, got["ユイ"].Usable())
		assert.ErrorIs(t, got["ユイ"].Cause, cause)
	}
}

func TestVoices_AFailureOfTheLookupItselfEndsIt(t *testing.T) {
	for _, err := range []error{voicerefsvc.ErrNotFound, errors.New("database is locked")} {
		l := &lookup{errs: map[string]error{"ユイ": err}}

		_, got := localspeech.Voices(context.Background(), l, "p1", "", []audiodirector.Scene{{Lines: []string{"ユイ: こんにちは"}}})

		assert.ErrorIs(t, got, err)
	}
}

func TestVoices_AScriptWithoutLinesNeedsNoLookup(t *testing.T) {
	l := &lookup{}

	got, err := localspeech.Voices(context.Background(), l, "p1", "", []audiodirector.Scene{{Index: 1}})

	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Empty(t, l.asked)
}

// The same refusals of the manager are the local lip sync's, tagged with its vendor: the Kind and the Detail are the same.
func TestUnavailableFor_TheVendorIsOnTheError(t *testing.T) {
	got := localspeech.UnavailableFor(context.Background(), localvoice.VendorMuseTalk, localruntime.ErrNotInstalled)

	var un *localvoice.UnavailableError
	require.ErrorAs(t, got, &un)
	assert.Equal(t, localvoice.VendorMuseTalk, un.Vendor)
	assert.Equal(t, localvoice.KindNotInstalled, un.Kind)
	assert.Contains(t, un.Error(), "MuseTalk")
	assert.ErrorIs(t, got, localruntime.ErrNotInstalled)

	voice := localspeech.Unavailable(context.Background(), localruntime.ErrNotInstalled)
	require.ErrorAs(t, voice, &un)
	assert.Empty(t, un.Vendor, "the voice has none")
	assert.Contains(t, un.Error(), "GPT-SoVITS")
}

func TestReason_AFailureOfTheLocalLipSyncIsWordedAsTheLipSyncAndAnUnknownProviderIsNamed(t *testing.T) {
	got, ok := localspeech.Reason(&localvoice.UnavailableError{Kind: localvoice.KindNotInstalled, Vendor: localvoice.VendorMuseTalk})
	require.True(t, ok)
	assert.Contains(t, got, "MuseTalk")
	assert.NotContains(t, got, "GPT-SoVITS")

	got, ok = localspeech.Reason(fmt.Errorf("produce: %w", localvoice.ErrUnknownLipSyncProvider))
	require.True(t, ok)
	assert.Contains(t, got, "口パク")
}
