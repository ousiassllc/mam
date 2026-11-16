// Package audiotest_test tests the audio input of a generation test (Issue #1153): a speech-to-video model (the S2V fake of gentesttest.WithInputModels) is
// given the sound and one picture, and the others are told that the sound is not theirs. It is a package of its own because servicetest is at its line limit.
package audiotest_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/audiofile/audiofiletest"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
)

var (
	s2v, t2v = string(gentesttest.S2VModel), string(gentesttest.T2VModel)
	// mp3 is the start of an MPEG-1 layer III file (an ID3 tag).
	mp3 = []byte("ID3\x04\x00\x00\x00\x00\x00\x00 a few frames of sound")
)

func videoReq(models []string, refs ...gentest.RefInput) gentest.Request {
	return gentest.Request{Media: "video", Models: models, Prompts: []gentest.PromptInput{{Prompt: "she says hello"}}, References: refs}
}

// picture stages a PNG with the role and audio stages the sound; both are refs of the request the way the screen and the MCP tool send them.
func picture(t *testing.T, r *gentesttest.Rig, role string) gentest.RefInput {
	t.Helper()
	id, _, err := r.Svc.StageReference(gentesttest.PNG)
	require.NoError(t, err)
	return gentest.RefInput{Source: gentest.FromStaged, ID: id, Role: role}
}

func audio(t *testing.T, r *gentesttest.Rig, data []byte) gentest.RefInput {
	t.Helper()
	id, _, err := r.Svc.StageAudio(data)
	require.NoError(t, err)
	return gentest.RefInput{Source: gentest.FromStaged, ID: id, Role: gentest.RoleAudio}
}

func failure(t *testing.T, err error) *apperr.Error {
	t.Helper()
	e, ok := apperr.As(err)
	require.True(t, ok, "not a common error: %v", err)
	return e
}

// invalid is the message of a refused input.
func invalid(t *testing.T, err error) string {
	t.Helper()
	var e *gterr.InvalidError
	require.ErrorAs(t, err, &e)
	return e.Message
}

func TestAStagedAudioFileHasTheTypeOfItsBytesAndTheExtensionOfItsType(t *testing.T) {
	r := gentesttest.New(t)
	for _, c := range []struct {
		data      []byte
		mime, ext string
	}{
		{gentesttest.WAV, "audio/wav", ".wav"}, {mp3, "audio/mpeg", ".mp3"},
		{audiofiletest.FLAC(44100, 44100), "audio/flac", ".flac"},
		{audiofiletest.OGGVorbis(44100, 44100), "audio/ogg", ".ogg"},
		{audiofiletest.M4A(0, 44100, 44100, true), "audio/mp4", ".m4a"},
	} {
		id, mime, err := r.Svc.StageAudio(c.data)
		require.NoError(t, err)
		assert.Equal(t, c.mime, mime)
		assert.Equal(t, c.ext, filepath.Ext(id))
		path, staged, err := r.Svc.StagedFile(id)
		require.NoError(t, err, "the staged file is served back (the screen's preview)")
		assert.Equal(t, c.mime, staged)
		got, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, c.data, got)
	}
	_, _, err := r.Svc.StageAudio(gentesttest.PNG)
	assert.Contains(t, invalid(t, err), "WAV・MP3・FLAC・OGG・M4A のどれか")
}

// The S2V model is given the sound as Audio (not as a reference) and the picture as its one reference; the batch keeps both as inputs.
func TestRunGivesTheSoundAndThePictureToTheSpeechToVideoModelAndKeepsBoth(t *testing.T) {
	for _, c := range []struct {
		name      string
		data      []byte
		mime, ext string
	}{
		{"wav", gentesttest.WAV, "audio/wav", ".wav"}, {"mp3", mp3, "audio/mpeg", ".mp3"},
		{"flac", audiofiletest.FLAC(44100, 44100), "audio/flac", ".flac"},
		{"ogg", audiofiletest.OGGOpus(312, 48000), "audio/ogg", ".ogg"},
		{"m4a", audiofiletest.M4A(0, 44100, 44100, true), "audio/mp4", ".m4a"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var crops atomic.Int32
			r := gentesttest.New(t, gentesttest.WithInputModels(), gentesttest.WithFaceCrop(func(context.Context, []byte) ([]byte, bool, error) {
				crops.Add(1)
				return nil, false, nil
			}))
			ctx := context.Background()
			pic, snd := picture(t, r, "reference"), audio(t, r, c.data)
			ref, err := r.Confirmed(videoReq([]string{s2v}, pic, snd))
			require.NoError(t, err)
			st := r.Wait(ref.BatchID)
			require.Equal(t, gentest.StateSucceeded, st.State, "%+v", st.Results)

			calls := r.Fake.CallsOf(gentesttest.S2VModel)
			require.Len(t, calls, 1)
			v := calls[0].Video
			require.NotNil(t, v.Audio, "the sound goes to the model as Audio")
			assert.Equal(t, c.data, v.Audio.Data)
			assert.Equal(t, c.mime, v.Audio.MimeType)
			require.Len(t, v.References, 1, "the picture is the one reference; the sound is not a reference")
			assert.Equal(t, gentesttest.PNG, v.References[0].Data)
			assert.Nil(t, v.FirstFrame)
			assert.Zero(t, crops.Load(), "no face cut is attempted for a sound")

			// the batch's record: an audio input beside the picture, in the order of the request
			inputs, err := r.Repo.Inputs(ctx, ref.BatchID)
			require.NoError(t, err)
			require.Len(t, inputs, 2)
			assert.Equal(t, gentestrepo.RoleReference, inputs[0].Role)
			sound := inputs[1]
			assert.Equal(t, "audio", sound.Role)
			assert.Equal(t, c.mime, sound.MimeType)
			assert.Equal(t, int64(len(c.data)), sound.SizeBytes)
			assert.Equal(t, 1, sound.Position)
			assert.Equal(t, c.ext, filepath.Ext(sound.FilePath), "the file keeps the extension ComfyUI reads the kind from")
			assert.JSONEq(t, `{"kind":"upload","id":"`+snd.ID+`","project_id":""}`, string(sound.Origin))
			f, err := r.Svc.InputFile(ctx, sound.ID)
			require.NoError(t, err)
			copied, err := os.ReadFile(f.Path)
			require.NoError(t, err)
			assert.Equal(t, c.data, copied, "the sound is copied into the batch")
			_, _, err = r.Svc.StagedFile(snd.ID)
			assert.ErrorIs(t, err, gterr.ErrNotFound, "the staged file is consumed")

			// the result used both
			assert.ElementsMatch(t, []string{inputs[0].ID, sound.ID}, st.Results[0].UsedInputs)
			assert.Empty(t, st.Results[0].UnusedInputs)
			d, err := r.Svc.Result(ctx, st.Results[0].ID)
			require.NoError(t, err)
			roles := map[string]string{}
			for _, in := range d.Inputs {
				roles[in.ID] = in.Role
			}
			assert.Equal(t, "audio", roles[sound.ID])
		})
	}
}

func TestTheSpeechToVideoModelStopsWithoutItsSoundOrItsPicture(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithInputModels())
	ctx := context.Background()

	noSound := videoReq([]string{s2v}, picture(t, r, "reference"))
	_, err := r.Svc.Estimate(ctx, noSound)
	e := failure(t, err)
	assert.Equal(t, apperr.CodeInputMissing, e.Code)
	assert.Equal(t, "audio", e.Args["param"])
	assert.Contains(t, e.View(apperr.LangJa).Cause, "音声ファイルが必要です")
	_, err = r.Svc.Run(ctx, noSound, gentest.Confirmation{})
	assert.Equal(t, apperr.CodeInputMissing, failure(t, err).Code, "Run stops too")

	_, err = r.Svc.Estimate(ctx, videoReq([]string{s2v}, audio(t, r, gentesttest.WAV)))
	e = failure(t, err)
	assert.Equal(t, apperr.CodeInputMissing, e.Code)
	assert.Equal(t, "reference_image", e.Args["param"], "the sound alone is not enough: the picture the face comes from is missing")

	_, err = r.Svc.Estimate(ctx, videoReq([]string{s2v}))
	assert.Equal(t, apperr.CodeInputMissing, failure(t, err).Code)
	assert.Empty(t, r.Fake.Calls(), "nothing was sent")
}

func TestASoundForAModelThatCannotUseItIsStoppedAloneAndWarnedAboutBesideTheOneThatCan(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithInputModels())
	ctx := context.Background()

	_, err := r.Svc.Estimate(ctx, videoReq([]string{t2v}, audio(t, r, gentesttest.WAV)))
	e := failure(t, err)
	assert.Equal(t, apperr.CodeInputUnused, e.Code, "the sound is of no use for a text-to-video model")

	pic, snd := picture(t, r, "reference"), audio(t, r, gentesttest.WAV)
	req := videoReq([]string{s2v, t2v}, pic, snd)
	est, err := r.Svc.Estimate(ctx, req)
	require.NoError(t, err, "one model uses the sound: the other only warns")
	var warned []string
	for _, w := range est.Warnings {
		if w.Code == gentest.WarnInputsDropped {
			warned = append(warned, w.Model)
		}
	}
	assert.NotContains(t, warned, s2v)
	assert.Contains(t, warned, t2v)
	var audioNote bool
	for _, w := range est.Warnings {
		audioNote = audioNote || w.Model == t2v && w.Code == gentest.WarnInputsDropped && strings.Contains(w.Message, "音声")
	}
	assert.True(t, audioNote, "the warning for the model says it is the sound it does not use: %+v", est.Warnings)

	ref, err := r.Svc.Run(ctx, req, gentest.Confirmation{EstimateID: est.ID, TotalUSD: est.TotalUSD})
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	require.Equal(t, gentest.StateSucceeded, st.State, "%+v", st.Results)
	by := map[string]record.ResultView{}
	for _, res := range st.Results {
		by[res.Model] = res
	}
	assert.Len(t, by[s2v].UsedInputs, 2)
	assert.Empty(t, by[s2v].UnusedInputs)
	assert.Empty(t, by[t2v].UsedInputs)
	var unusedSound *record.UnusedInput
	for i, u := range by[t2v].UnusedInputs {
		if u.Role == "audio" {
			unusedSound = &by[t2v].UnusedInputs[i]
		}
	}
	require.NotNil(t, unusedSound, "the model's result records the sound it was not given: %+v", by[t2v].UnusedInputs)
	assert.Equal(t, "文章だけから作るモデルなので、画像・音声は使われません", unusedSound.Reason)

	require.Len(t, r.Fake.CallsOf(gentesttest.T2VModel), 1)
	assert.Nil(t, r.Fake.CallsOf(gentesttest.T2VModel)[0].Video.Audio, "the text-to-video model was not given the sound")
	assert.NotNil(t, r.Fake.CallsOf(gentesttest.S2VModel)[0].Video.Audio)
}
