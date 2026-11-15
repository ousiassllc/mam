package audiotest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// A model that takes pictures but no sound (an image-to-video model) is given the picture and warned about the sound: the reason says it is the sound.
func TestAnImageToVideoModelDropsTheSoundButKeepsThePicture(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithInputModels())
	i2v := string(gentesttest.I2VModel)
	req := videoReq([]string{i2v}, picture(t, r, "first_frame"), audio(t, r, gentesttest.WAV))
	ref, err := r.Confirmed(req)
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	require.Equal(t, gentest.StateSucceeded, st.State, "%+v", st.Results)

	v := r.Fake.CallsOf(gentesttest.I2VModel)[0].Video
	assert.NotNil(t, v.FirstFrame)
	assert.Nil(t, v.Audio)
	require.Len(t, st.Results[0].UnusedInputs, 1)
	assert.Equal(t, "audio", st.Results[0].UnusedInputs[0].Role)
	assert.Equal(t, "このモデルは音声を使えません", st.Results[0].UnusedInputs[0].Reason)
	assert.Len(t, st.Results[0].UsedInputs, 1)
}

func TestTheAudioInputIsRefusedWhereItCannotBeUsed(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithInputModels())
	ctx := context.Background()
	two := func() gentest.Request {
		return videoReq([]string{s2v}, picture(t, r, "reference"), audio(t, r, gentesttest.WAV), audio(t, r, gentesttest.WAV))
	}
	asImage := func() gentest.Request {
		q := gentest.Request{Media: "image", Models: []string{"nano-banana"}, Prompts: []gentest.PromptInput{{Prompt: "a fox"}}}
		q.References = []gentest.RefInput{audio(t, r, gentesttest.WAV)}
		return q
	}
	pngAsSound := func() gentest.Request {
		ref := picture(t, r, gentest.RoleAudio)
		return videoReq([]string{s2v}, picture(t, r, "reference"), ref)
	}
	// the sound with a role of a picture: the model does not need a sound, so only the file itself says it is no picture
	soundAs := func(role string) func() gentest.Request {
		return func() gentest.Request {
			snd := audio(t, r, gentesttest.WAV)
			snd.Role = role
			return videoReq([]string{string(gentesttest.I2VModel)}, snd)
		}
	}
	soundAsImage := func() gentest.Request {
		q := asImage()
		q.References[0].Role = "reference"
		return q
	}
	from := func(source string) func() gentest.Request {
		return func() gentest.Request {
			return videoReq([]string{s2v}, picture(t, r, "reference"), gentest.RefInput{Source: source, ID: "x1234567", Role: gentest.RoleAudio})
		}
	}
	cases := []struct {
		name string
		req  func() gentest.Request
		want string
	}{
		{"two sounds", two, "音声ファイルは 1 つまで"},
		{"a sound for an image batch", asImage, "音声は動画だけ"},
		{"a picture as the sound", pngAsSound, "WAV か MP3"},
		{"a sound as a first frame", soundAs("first_frame"), "PNG・JPEG・WebP"},
		{"a sound as a reference picture of an image batch", soundAsImage, "PNG・JPEG・WebP"},
		{"a sound from a Project asset", from(gentest.FromProjectAsset), "upload か batch_input"},
		{"a sound from the history", from(gentest.FromHistory), "upload か batch_input"},
		{"a sound from the pose library", from(gentest.FromPoseLibrary), "upload か batch_input"},
		{"a sound from a reference item", from(gentest.FromReferenceItem), "upload か batch_input"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := r.Svc.Estimate(ctx, c.req())
			assert.Contains(t, invalid(t, err), c.want)
			_, err = r.Svc.Run(ctx, c.req(), gentest.Confirmation{})
			assert.Error(t, err, "Run refuses it too")
		})
	}
	assert.Empty(t, r.Fake.Calls(), "nothing was sent")
	var batches int
	require.NoError(t, r.DB.Reader().QueryRow(`SELECT COUNT(*) FROM gentest_batch`).Scan(&batches))
	assert.Zero(t, batches, "no batch was made")
}

// A sound of an earlier batch is an input again (the redo of a run, "この設定でもう一度"): the model gets the same bytes, and the origin says where it came from.
func TestTheSoundOfAnEarlierBatchIsAnInputAgain(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithInputModels())
	ctx := context.Background()
	first, err := r.Confirmed(videoReq([]string{s2v}, picture(t, r, "reference"), audio(t, r, mp3)))
	require.NoError(t, err)
	st := r.Wait(first.BatchID)
	require.Equal(t, gentest.StateSucceeded, st.State)
	d, err := r.Svc.Result(ctx, st.Results[0].ID)
	require.NoError(t, err)
	var sound, picture1 string
	for _, in := range d.Inputs {
		if in.Role == "audio" {
			sound = in.ID
		} else {
			picture1 = in.ID
		}
	}
	require.NotEmpty(t, sound)

	again := videoReq([]string{s2v}, gentest.RefInput{Source: gentest.FromBatchInput, ID: picture1, Role: "reference"}, gentest.RefInput{Source: gentest.FromBatchInput, ID: sound, Role: gentest.RoleAudio})
	second, err := r.Confirmed(again)
	require.NoError(t, err)
	st2 := r.Wait(second.BatchID)
	require.Equal(t, gentest.StateSucceeded, st2.State, "%+v", st2.Results)
	calls := r.Fake.CallsOf(gentesttest.S2VModel)
	require.Len(t, calls, 2)
	require.NotNil(t, calls[1].Video.Audio)
	assert.Equal(t, mp3, calls[1].Video.Audio.Data)
	assert.Equal(t, "audio/mpeg", calls[1].Video.Audio.MimeType, "the type is the stored one")
	d2, err := r.Svc.Result(ctx, st2.Results[0].ID)
	require.NoError(t, err)
	for _, in := range d2.Inputs {
		if in.Role == "audio" {
			assert.Equal(t, gentest.FromBatchInput, in.Origin.Kind)
			assert.Equal(t, sound, in.Origin.ID)
			assert.Equal(t, "audio/mpeg", in.MimeType)
		}
	}

	// the stored sound is not a picture, and a stored picture is not a sound
	bad := videoReq([]string{s2v}, gentest.RefInput{Source: gentest.FromBatchInput, ID: picture1, Role: "reference"}, gentest.RefInput{Source: gentest.FromBatchInput, ID: picture1, Role: gentest.RoleAudio})
	_, err = r.Svc.Estimate(ctx, bad)
	var invalidErr *gterr.InvalidError
	require.ErrorAs(t, err, &invalidErr)
	assert.Contains(t, invalidErr.Message, "WAV か MP3")
	bad = videoReq([]string{string(gentesttest.I2VModel)}, gentest.RefInput{Source: gentest.FromBatchInput, ID: sound, Role: "first_frame"})
	_, err = r.Svc.Estimate(ctx, bad)
	require.ErrorAs(t, err, &invalidErr)
	assert.Contains(t, invalidErr.Message, "PNG・JPEG・WebP")
	assert.Len(t, r.Fake.CallsOf(domain.ModelName(s2v)), 2)
}
