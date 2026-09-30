package media

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeTone writes an audio file of a sine tone (amplitude: 0..1) of the given length.
func makeTone(t *testing.T, tools Tools, name string, seconds float64, amplitude float64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	out, err := exec.Command(tools.ffmpeg, "-v", "error", "-y", "-f", "lavfi", "-i",
		"sine=frequency=330:duration="+strconv.FormatFloat(seconds, 'f', -1, 64), "-af", "volume="+strconv.FormatFloat(amplitude, 'f', -1, 64), path).CombinedOutput()
	require.NoError(t, err, string(out))
	return path
}

func TestRender_HardCutsAndTransitionsSetTheLengthOfTheVideo(t *testing.T) {
	tools := lookOrSkip(t)
	a := makeVideo(t, tools, "a.mp4", 320, 180, 24, 4, true)
	b := makeVideo(t, tools, "b.mp4", 320, 180, 24, 4, true)
	cuts := []Cut{
		{Path: a, Start: 0, End: 2},
		{Path: b, Start: 1, End: 3},                                   // a hard cut: no overlap
		{Path: a, Start: 2, End: 4, Transition: "fade", Overlap: 0.5}, // 0.5 s shorter
	}
	dst := filepath.Join(t.TempDir(), "out", "cut.mp4")

	info, err := tools.Render(context.Background(), cuts, RenderOptions{Draft: true}, dst)

	require.NoError(t, err)
	assert.InDelta(t, 5.5, info.Duration, 0.2)
	assert.InDelta(t, RenderedDuration(cuts), info.Duration, 0.2)
	assert.True(t, info.HasAudio, "every cut has sound, so the result keeps it")
}

func TestRender_ScalesToTheAskedSizeAndCodec(t *testing.T) {
	tools := lookOrSkip(t)
	a := makeVideo(t, tools, "a.mp4", 320, 180, 24, 2, false)
	tests := []struct {
		codec, ext string
	}{{CodecH264, "mp4"}, {CodecH265, "mp4"}, {CodecVP9, "webm"}}
	for _, tc := range tests {
		t.Run(tc.codec, func(t *testing.T) {
			dst := filepath.Join(t.TempDir(), "v."+tc.ext)

			info, err := tools.Render(context.Background(), []Cut{{Path: a, Start: 0, End: 1}}, RenderOptions{Width: 180, Height: 320, Codec: tc.codec, Draft: true}, dst)

			require.NoError(t, err)
			assert.Equal(t, 180, info.Width)
			assert.Equal(t, 320, info.Height)
			assert.False(t, info.HasAudio)
		})
	}
}

func TestRender_LaysAFinishedAudioFileUnderThePicture(t *testing.T) {
	tools := lookOrSkip(t)
	a := makeVideo(t, tools, "a.mp4", 320, 180, 24, 3, false)
	tone := makeTone(t, tools, "tone.wav", 3, 0.5)
	dst := filepath.Join(t.TempDir(), "v.mp4")

	info, err := tools.Render(context.Background(), []Cut{{Path: a, Start: 0, End: 2}}, RenderOptions{Audio: tone, Draft: true}, dst)

	require.NoError(t, err)
	assert.True(t, info.HasAudio)
	assert.InDelta(t, 2, info.Duration, 0.2, "the picture decides the length")
}

func TestRender_FailureLeavesNoFileAtTheDestination(t *testing.T) {
	tools := lookOrSkip(t)
	a := makeVideo(t, tools, "a.mp4", 320, 180, 24, 2, false)
	dst := filepath.Join(t.TempDir(), "v.mp4")

	_, err := tools.Render(context.Background(), []Cut{{Path: a, Start: 5, End: 6}}, RenderOptions{}, dst)

	require.Error(t, err)
	_, statErr := os.Stat(dst)
	assert.True(t, os.IsNotExist(statErr))
}

func TestRender_UnknownCodecFails(t *testing.T) {
	tools := lookOrSkip(t)
	a := makeVideo(t, tools, "a.mp4", 320, 180, 24, 2, false)

	_, err := tools.Render(context.Background(), []Cut{{Path: a, Start: 0, End: 1}}, RenderOptions{Codec: "av2"}, filepath.Join(t.TempDir(), "v.mp4"))

	assert.ErrorContains(t, err, "unknown codec")
}

func TestOverlapOf_IsCappedAtHalfOfTheShorterCut(t *testing.T) {
	cuts := []Cut{{End: 4}, {End: 1, Transition: "fade", Overlap: 2}, {End: 4, Transition: "cut", Overlap: 1}}

	assert.Equal(t, 0.0, overlapOf(cuts, 0))
	assert.Equal(t, 0.5, overlapOf(cuts, 1))
	assert.Equal(t, 0.0, overlapOf(cuts, 2), "a hard cut has no overlap")
}

func TestMaster_NormalizesTheMixToTheTargetLoudness(t *testing.T) {
	tools := lookOrSkip(t)
	base := makeVideo(t, tools, "base.mp4", 320, 180, 24, 4, true)
	voice := makeTone(t, tools, "voice.wav", 1.5, 0.2)
	dst := filepath.Join(t.TempDir(), "audio", "mix.m4a")

	got, err := tools.Master(context.Background(), base, []Track{{Path: voice, Start: 1}}, 4, -14, dst)

	require.NoError(t, err)
	assert.InDelta(t, -14, got.Integrated, 1.0)
	assert.LessOrEqual(t, got.TruePeak, TruePeakCeiling+0.5)
	d, err := tools.AudioDuration(context.Background(), dst)
	require.NoError(t, err)
	assert.InDelta(t, 4, d, 0.2, "the mix is as long as the video")
}

func TestMaster_VoicesAloneMakeTheMix(t *testing.T) {
	tools := lookOrSkip(t)
	voice := makeTone(t, tools, "voice.wav", 1, 0.05) // very quiet: gain is added
	dst := filepath.Join(t.TempDir(), "mix.m4a")

	got, err := tools.Master(context.Background(), "", []Track{{Path: voice, Start: 0}}, 2, -14, dst)

	require.NoError(t, err)
	assert.InDelta(t, -14, got.Integrated, 1.5)
}

func TestMaster_NothingToMixIsSilent(t *testing.T) {
	tools := lookOrSkip(t)

	_, err := tools.Master(context.Background(), "", nil, 2, -14, filepath.Join(t.TempDir(), "mix.m4a"))

	assert.ErrorIs(t, err, ErrSilent)
}
