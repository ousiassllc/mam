// Package audiolentest tests how a generation test treats the length of the audio input for a model whose clip is as long as its sound (Issue #1153): the estimate reads it from
// the file and shows it as the length, the confirmation has no length to round, a sound over gentest.max_audio_seconds or unreadable is refused, and the result records the real
// length. It is a package of its own because servicetest is at its line limit.
package audiolentest_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/audiochunks"
	"github.com/ousiassllc/mam/backend/internal/audiofile/audiofiletest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
)

var s2v, t2v = string(gentesttest.S2VModel), string(gentesttest.T2VModel)

// withAudioLength makes the rig's S2V model one whose clip is as long as its sound (the shipped template has audio_chunks; the fake backend ignores the graph) and sets the limit.
func withAudioLength(limit int) gentesttest.Option {
	return func(r *gentesttest.Rig, _ *gentest.Deps) {
		r.Config.LocalModels[localcfg.Key(gentesttest.EndpointName, "s2v")].Workflow.AudioChunks = &audiochunks.Spec{ChunkLength: 77}
		r.Config.Gentest.MaxAudioSeconds = &limit
	}
}

func rig(t *testing.T, limit int) *gentesttest.Rig {
	t.Helper()
	return gentesttest.New(t, gentesttest.WithInputModels(), withAudioLength(limit))
}

// wav is a sound of the given number of milliseconds.
func wav(ms int) []byte { return audiofiletest.WAV(8000, 8*ms) }

func request(t *testing.T, r *gentesttest.Rig, models []string, sound []byte) gentest.Request {
	t.Helper()
	pic, _, err := r.Svc.StageReference(gentesttest.PNG)
	require.NoError(t, err)
	snd, _, err := r.Svc.StageAudio(sound)
	require.NoError(t, err)
	return gentest.Request{
		Media: "video", Models: models, Prompts: []gentest.PromptInput{{Prompt: "she says hello"}}, DurationSeconds: 5,
		References: []gentest.RefInput{{Source: gentest.FromStaged, ID: pic, Role: "reference"}, {Source: gentest.FromStaged, ID: snd, Role: gentest.RoleAudio}},
	}
}

func lineOf(t *testing.T, e gentest.Estimate, model string) gentest.ModelEstimate {
	t.Helper()
	for _, l := range e.Breakdown {
		if l.Model == model {
			return l
		}
	}
	require.Failf(t, "no line", "%s", model)
	return gentest.ModelEstimate{}
}

// The estimate's length of an audio-length model is the sound's (the request's 5 s is the form's default and means nothing to it), without the warning of a rounded length.
func TestTheEstimateShowsTheLengthOfTheSoundAndDoesNotRoundIt(t *testing.T) {
	r := rig(t, 60)
	est, err := r.Svc.Estimate(t.Context(), request(t, r, []string{s2v}, wav(3200)))
	require.NoError(t, err)
	assert.InDelta(t, 3.2, lineOf(t, est, s2v).Seconds, 1e-9)
	for _, w := range est.Warnings {
		assert.NotEqual(t, gentest.WarnDurationSnapped, w.Code, w.Message)
	}

	// the same request for a 42.34 s sound: rounded up to a tenth of a second, the length the clip is recorded with
	est, err = r.Svc.Estimate(t.Context(), request(t, r, []string{s2v}, wav(42340)))
	require.NoError(t, err)
	assert.InDelta(t, 42.4, lineOf(t, est, s2v).Seconds, 1e-9)
}

// In a batch with another model, each has its own length: the sound's for the audio-length one, the request's (rounded as always) for the other.
func TestAMixedBatchHasALengthPerModel(t *testing.T) {
	r := rig(t, 60)
	req := request(t, r, []string{s2v, t2v}, wav(3200))
	req.DurationSeconds = 7
	est, err := r.Svc.Estimate(t.Context(), req)
	require.NoError(t, err)
	assert.InDelta(t, 3.2, lineOf(t, est, s2v).Seconds, 1e-9)
	assert.InDelta(t, 5.0, lineOf(t, est, t2v).Seconds, 1e-9, "the other model still snaps 7 s to its 5 s")
	var snapped []string
	for _, w := range est.Warnings {
		if w.Code == gentest.WarnDurationSnapped {
			snapped = append(snapped, w.Model)
		}
	}
	assert.Equal(t, []string{t2v}, snapped, "only the model with lengths to choose from is told its length is rounded")
}

// A sound over the limit or whose length cannot be read is refused by the estimate (and so by Run) with the sentences of direct.Video, before anything is priced.
func TestASoundOverTheLimitOrUnreadableIsRefusedByTheEstimate(t *testing.T) {
	r := rig(t, 10)
	_, err := r.Svc.Estimate(t.Context(), request(t, r, []string{s2v}, wav(10100)))
	var invalid *gterr.InvalidError
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, "音声が長すぎます（10.1 秒。上限は 10 秒）", invalid.Message)

	_, err = r.Svc.Run(t.Context(), request(t, r, []string{s2v}, wav(10100)), gentest.Confirmation{})
	assert.ErrorAs(t, err, &invalid, "Run refuses it too")

	_, err = r.Svc.Estimate(t.Context(), request(t, r, []string{s2v}, wav(10000)))
	assert.NoError(t, err, "the limit itself is allowed")

	// a WAV whose header is whole but whose data chunk is missing has no length
	broken := append([]byte("RIFF\x24\x00\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00\x40\x1f\x00\x00\x80\x3e\x00\x00\x02\x00\x10\x00"), []byte("LIST\x00\x00\x00\x00")...)
	_, err = r.Svc.Estimate(t.Context(), request(t, r, []string{s2v}, broken))
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, "音声の長さを読めません", invalid.Message)
	assert.Empty(t, r.Fake.Calls(), "nothing was sent")
}

// A model whose clip does not follow its sound does not read the sound's length: the same sound is no problem for it.
func TestTheLimitAppliesOnlyToAModelWhoseClipIsAsLongAsTheSound(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithInputModels(), func(r *gentesttest.Rig, _ *gentest.Deps) {
		limit := 10
		r.Config.Gentest.MaxAudioSeconds = &limit
	})
	est, err := r.Svc.Estimate(t.Context(), request(t, r, []string{s2v}, wav(30000)))
	require.NoError(t, err, "the S2V fake of the rig is a model with a clip of 5 s")
	assert.InDelta(t, 5.0, lineOf(t, est, s2v).Seconds, 1e-9)
}

// The run records the real length with the result, the model is called with the sound's length, and a second run of another sound is another length of its own.
func TestTheResultRecordsTheLengthOfTheSound(t *testing.T) {
	r := rig(t, 60)
	ref, err := r.Confirmed(request(t, r, []string{s2v}, wav(12300)))
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	require.Equal(t, gentest.StateSucceeded, st.State, "%+v", st.Results)

	calls := r.Fake.CallsOf(gentesttest.S2VModel)
	require.Len(t, calls, 1)
	assert.InDelta(t, 12.3, calls[0].Video.DurationSeconds, 1e-9, "direct.Video gives the backend the sound's length")

	res, err := r.Repo.Results(t.Context(), ref.BatchID)
	require.NoError(t, err)
	require.Len(t, res, 1)
	var p record.ResultParams
	require.NoError(t, json.Unmarshal(res[0].Params, &p))
	assert.InDelta(t, 12.3, p.DurationSeconds, 1e-9)
}

// The model list (the screen's, and list_generation_models of the MCP tools, which marshal this) says the clip follows the sound and has no lengths to choose from.
func TestTheModelListSaysTheLengthFollowsTheSound(t *testing.T) {
	r := rig(t, 60)
	by := map[string]modelcatalog.Info{}
	for _, m := range r.Svc.Models(t.Context(), "video") {
		by[m.ID] = m
	}
	require.NotNil(t, by[s2v].Video)
	assert.True(t, by[s2v].Video.LengthFromAudio)
	assert.Empty(t, by[s2v].Video.Durations, "no length to choose")
	raw, err := json.Marshal(by[s2v].Video)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"length_from_audio":true`)
	assert.Contains(t, string(raw), `"durations":[]`, "an empty list, not a missing field")

	require.NotNil(t, by[t2v].Video)
	assert.False(t, by[t2v].Video.LengthFromAudio)
	assert.NotEmpty(t, by[t2v].Video.Durations)
}
