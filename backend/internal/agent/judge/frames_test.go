package judge_test

import (
	"bytes"
	"context"
	"image"
	_ "image/jpeg" // decoder of the sampled frames
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/judge"
)

// testVideo makes a 2-second, 30 fps, 320×180 clip with ffmpeg.
func testVideo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	path := filepath.Join(t.TempDir(), "take.mp4")
	out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc=size=320x180:rate=30:duration=2", "-pix_fmt", "yuv420p", path).CombinedOutput() // #nosec G204 -- test
	require.NoError(t, err, string(out))
	return path
}

func TestFFmpeg_SamplesEvenlySpacedFramesAndNumbersThemBySourceFrame(t *testing.T) {
	ff, err := judge.LookFFmpeg()
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	clip, err := ff.Sample(context.Background(), testVideo(t), judge.SampleSpec{FPS: 4, MaxFrames: 48, MaxWidth: 160})

	require.NoError(t, err)
	assert.InDelta(t, 2.0, clip.Duration, 0.1)
	assert.InDelta(t, 30.0, clip.FPS, 0.01)
	assert.InDelta(t, 0.25, clip.Interval, 1e-9)
	require.GreaterOrEqual(t, len(clip.Frames), 7)
	require.LessOrEqual(t, len(clip.Frames), 9)
	assert.Equal(t, 0, clip.Frames[0].Number)
	assert.InDelta(t, 0.25, clip.Frames[1].Time, 1e-9)
	assert.Equal(t, 8, clip.Frames[1].Number, "0.25 s at 30 fps")
	img, _, err := image.Decode(bytes.NewReader(clip.Frames[0].JPEG))
	require.NoError(t, err)
	assert.Equal(t, 160, img.Bounds().Dx(), "scaled down to the maximum width")
}

func TestFFmpeg_ALongTakeIsSampledMoreSparselyThanTheFrameLimit(t *testing.T) {
	ff, err := judge.LookFFmpeg()
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	clip, err := ff.Sample(context.Background(), testVideo(t), judge.SampleSpec{FPS: 30, MaxFrames: 5, MaxWidth: 1000})

	require.NoError(t, err)
	assert.LessOrEqual(t, len(clip.Frames), 5)
	assert.InDelta(t, 0.4, clip.Interval, 1e-9, "5 frames over 2 seconds")
	img, _, err := image.Decode(bytes.NewReader(clip.Frames[0].JPEG))
	require.NoError(t, err)
	assert.Equal(t, 320, img.Bounds().Dx(), "a frame narrower than the maximum is not enlarged")
}

func TestFFmpeg_AFileThatIsNotAVideoIsAnError(t *testing.T) {
	ff, err := judge.LookFFmpeg()
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	bad := filepath.Join(t.TempDir(), "bad.mp4")
	require.NoError(t, writeFile(bad, []byte("\x00\x00\x00\x18ftypisom")))
	_, err = ff.Sample(context.Background(), bad, judge.SampleSpec{FPS: 4, MaxFrames: 10, MaxWidth: 100})
	assert.Error(t, err)
}

func TestClip_FrameRangeIsInclusiveAndClamped(t *testing.T) {
	c := judge.Clip{Duration: 5, FPS: 30}
	first, last := c.FrameRange(2.1, 2.8)
	assert.Equal(t, []int{63, 84}, []int{first, last}, "the example of spec §12.3")
	first, last = c.FrameRange(4.5, 9)
	assert.Equal(t, []int{135, 149}, []int{first, last}, "clamped to the last frame of the Take")
	first, last = c.FrameRange(-1, 0)
	assert.Equal(t, []int{0, 0}, []int{first, last})
}
