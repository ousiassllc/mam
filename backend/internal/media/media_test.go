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

	"github.com/ousiassllc/mam/backend/internal/testenv"
)

// lookOrSkip returns the tools, or skips the test on a machine without ffmpeg.
func lookOrSkip(t *testing.T) Tools {
	t.Helper()
	tools, err := Look()
	if err != nil {
		testenv.MissingFFmpeg(t, "ffmpeg / ffprobe are not installed")
	}
	return tools
}

// makeVideo writes a test video of the given size, frame rate and length (with a tone when audio).
func makeVideo(t *testing.T, tools Tools, name string, w, h int, fps float64, seconds float64, audio bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	args := []string{"-v", "error", "-y", "-f", "lavfi", "-i",
		"testsrc=size=" + strconv.Itoa(w) + "x" + strconv.Itoa(h) + ":rate=" + strconv.FormatFloat(fps, 'f', -1, 64) + ":duration=" + strconv.FormatFloat(seconds, 'f', -1, 64)}
	if audio {
		args = append(args, "-f", "lavfi", "-i", "sine=frequency=440:duration="+strconv.FormatFloat(seconds, 'f', -1, 64), "-c:a", "aac")
	}
	args = append(args, "-c:v", "libx264", "-pix_fmt", "yuv420p", path)
	out, err := exec.Command(tools.ffmpeg, args...).CombinedOutput()
	require.NoError(t, err, string(out))
	return path
}

func TestProbe_ReadsSizeRateLengthAndAudio(t *testing.T) {
	tools := lookOrSkip(t)
	path := makeVideo(t, tools, "a.mp4", 320, 180, 30, 2, true)

	info, err := tools.Probe(context.Background(), path)

	require.NoError(t, err)
	assert.Equal(t, 320, info.Width)
	assert.Equal(t, 180, info.Height)
	assert.InDelta(t, 30, info.FPS, 0.01)
	assert.InDelta(t, 2, info.Duration, 0.1)
	assert.True(t, info.HasAudio)
}

func TestProbe_FileWithoutVideoFails(t *testing.T) {
	tools := lookOrSkip(t)
	path := filepath.Join(t.TempDir(), "not-a-video.mp4")
	require.NoError(t, os.WriteFile(path, []byte("nothing"), 0o600))

	_, err := tools.Probe(context.Background(), path)

	assert.Error(t, err)
}

func TestCompose_JoinsSegmentsWithACrossFadeAtTheJoint(t *testing.T) {
	tools := lookOrSkip(t)
	a := makeVideo(t, tools, "a.mp4", 320, 180, 24, 4, true)
	b := makeVideo(t, tools, "b.mp4", 640, 360, 30, 4, true)
	dst := filepath.Join(t.TempDir(), "out", "joined.mp4")
	segs := []Segment{{Path: a, Start: 0, End: 2}, {Path: b, Start: 2, End: 4}}

	info, err := tools.Compose(context.Background(), segs, dst)

	require.NoError(t, err)
	assert.Equal(t, 320, info.Width, "the picture takes the size of the first segment")
	assert.InDelta(t, 24, info.FPS, 0.01, "and its frame rate")
	assert.InDelta(t, JoinedDuration(segs), info.Duration, 0.15, "the two seconds overlap by the fade")
	assert.InDelta(t, 3.75, info.Duration, 0.15)
	assert.True(t, info.HasAudio)
	assert.NoFileExists(t, filepath.Join(filepath.Dir(dst), ".joined.mp4.ffmpeg.tmp"))
}

func TestCompose_WithoutSoundInOneFileDropsTheSoundOfAll(t *testing.T) {
	tools := lookOrSkip(t)
	a := makeVideo(t, tools, "a.mp4", 320, 180, 24, 3, true)
	b := makeVideo(t, tools, "b.mp4", 320, 180, 24, 3, false)
	dst := filepath.Join(t.TempDir(), "joined.mp4")

	info, err := tools.Compose(context.Background(), []Segment{{Path: a, Start: 0, End: 1.5}, {Path: b, Start: 1, End: 3}}, dst)

	require.NoError(t, err)
	assert.False(t, info.HasAudio)
	assert.InDelta(t, 3.25, info.Duration, 0.15)
}

func TestCompose_SingleSegmentIsACut(t *testing.T) {
	tools := lookOrSkip(t)
	a := makeVideo(t, tools, "a.mp4", 320, 180, 24, 4, true)
	dst := filepath.Join(t.TempDir(), "cut.mp4")

	info, err := tools.Compose(context.Background(), []Segment{{Path: a, Start: 1, End: 3}}, dst)

	require.NoError(t, err)
	assert.InDelta(t, 2, info.Duration, 0.15)
}

func TestCompose_SegmentPastTheEndOfItsFileIsCutAtTheEnd(t *testing.T) {
	tools := lookOrSkip(t)
	a := makeVideo(t, tools, "a.mp4", 320, 180, 24, 2, false)
	dst := filepath.Join(t.TempDir(), "cut.mp4")

	info, err := tools.Compose(context.Background(), []Segment{{Path: a, Start: 1, End: 9}}, dst)

	require.NoError(t, err)
	assert.InDelta(t, 1, info.Duration, 0.15)
}

func TestCompose_EmptySegmentFails(t *testing.T) {
	tools := lookOrSkip(t)
	a := makeVideo(t, tools, "a.mp4", 320, 180, 24, 2, false)

	_, err := tools.Compose(context.Background(), []Segment{{Path: a, Start: 5, End: 6}}, filepath.Join(t.TempDir(), "x.mp4"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestFadeFor_ShrinksWhenASegmentIsShort(t *testing.T) {
	assert.Zero(t, fadeFor([]Segment{{End: 5}}))
	assert.InDelta(t, FadeSeconds, fadeFor([]Segment{{End: 5}, {End: 5}}), 1e-9)
	assert.InDelta(t, 0.1, fadeFor([]Segment{{End: 5}, {Start: 1, End: 1.3}}), 1e-9)
}

func TestProxy_ScalesA4KVideoDownTo1080AndThumbnailWritesAJPEG(t *testing.T) {
	tools := lookOrSkip(t)
	src := makeVideo(t, tools, "uhd.mp4", 3840, 2160, 12, 1, true)
	dir := t.TempDir()

	require.NoError(t, tools.Proxy(context.Background(), src, filepath.Join(dir, "proxy.mp4")))
	require.NoError(t, tools.Thumbnail(context.Background(), src, filepath.Join(dir, "poster.jpg"), 0.5))

	info, err := tools.Probe(context.Background(), filepath.Join(dir, "proxy.mp4"))
	require.NoError(t, err)
	assert.Equal(t, MaxPreviewHeight, info.Height)
	assert.Equal(t, 1920, info.Width)
	jpg, err := os.ReadFile(filepath.Join(dir, "poster.jpg"))
	require.NoError(t, err)
	assert.Equal(t, []byte{0xff, 0xd8}, jpg[:2])
}

// makeVideoWithLongerAudio is a video of videoSeconds with a tone of audioSeconds, so the container
// is longer than the picture.
func makeVideoWithLongerAudio(t *testing.T, tools Tools, name string, videoSeconds, audioSeconds float64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	out, err := exec.Command(tools.ffmpeg, "-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=64x64:rate=10:duration="+strconv.FormatFloat(videoSeconds, 'f', -1, 64),
		"-f", "lavfi", "-i", "sine=frequency=440:duration="+strconv.FormatFloat(audioSeconds, 'f', -1, 64),
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", path).CombinedOutput()
	require.NoError(t, err, string(out))
	return path
}

// ffmpeg exits 0 and writes nothing when -ss is past the last video frame, which is where
// Info.Duration - 0.1 lies for a Take whose audio is longer than its picture (the final frame of
// the Continue Agent).
func TestThumbnail_AFrameIsFoundWhenTheSecondIsPastTheLastVideoFrame(t *testing.T) {
	tools := lookOrSkip(t)
	src := makeVideoWithLongerAudio(t, tools, "long-audio.mp4", 1, 2)
	info, err := tools.Probe(context.Background(), src)
	require.NoError(t, err)
	require.Greater(t, info.Duration, 1.5, "the container is longer than the picture")

	for _, at := range []float64{info.Duration - 0.1, 5, 1000, -3} {
		dst := filepath.Join(t.TempDir(), "final.jpg")
		require.NoError(t, tools.Thumbnail(context.Background(), src, dst, at), "at %v", at)
		jpg, err := os.ReadFile(dst)
		require.NoError(t, err)
		assert.Equal(t, []byte{0xff, 0xd8}, jpg[:2], "at %v", at)
	}
}

func TestThumbnail_ASecondInsideTheVideoStillTakesThatFrame(t *testing.T) {
	tools := lookOrSkip(t)
	src := makeVideo(t, tools, "a.mp4", 64, 64, 10, 2, false)
	dst := filepath.Join(t.TempDir(), "mid.jpg")

	require.NoError(t, tools.Thumbnail(context.Background(), src, dst, 1))
	_, err := os.Stat(dst)
	assert.NoError(t, err)
}

// makeChangingVideo is a 1 s, 10 fps video whose every frame differs (a zooming Mandelbrot set),
// with a tone of audioSeconds when that is more than 0.
func makeChangingVideo(t *testing.T, tools Tools, name string, audioSeconds float64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	args := []string{"-v", "error", "-y", "-f", "lavfi", "-t", "1", "-i", "mandelbrot=size=64x64:rate=10"}
	if audioSeconds > 0 {
		args = append(args, "-f", "lavfi", "-i", "sine=frequency=440:duration="+strconv.FormatFloat(audioSeconds, 'f', -1, 64), "-c:a", "aac")
	}
	args = append(args, "-c:v", "libx264", "-pix_fmt", "yuv420p", path)
	out, err := exec.Command(tools.ffmpeg, args...).CombinedOutput()
	require.NoError(t, err, string(out))
	return path
}

func thumbnailBytes(t *testing.T, tools Tools, src string, at float64) []byte {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "frame.jpg")
	require.NoError(t, tools.Thumbnail(context.Background(), src, dst, at), "at %v", at)
	jpg, err := os.ReadFile(dst)
	require.NoError(t, err)
	return jpg
}

// A second past the end is the last frame of the picture, not the first one, however much longer
// the audio is than the picture (a back-off of a few seconds never reaches the picture of a Take
// whose audio is 20 s long).
func TestThumbnail_ASecondPastTheEndTakesTheLastVideoFrame(t *testing.T) {
	tools := lookOrSkip(t)
	for name, audioSeconds := range map[string]float64{"audio as long as the video": 1, "audio longer than 8 s": 20} {
		t.Run(name, func(t *testing.T) {
			src := makeChangingVideo(t, tools, "past-end.mp4", audioSeconds)
			last := thumbnailBytes(t, tools, src, 0.85) // the last frame of the 10 fps picture is at 0.9, the first one from 0.85 on
			first := thumbnailBytes(t, tools, src, 0)
			require.NotEqual(t, first, last, "the first and the last frame of the test video differ")

			for _, at := range []float64{audioSeconds - 0.1, audioSeconds + 5, 1000} {
				assert.Equal(t, last, thumbnailBytes(t, tools, src, at), "at %v", at)
			}
		})
	}
}

// A container with no length of its own for the picture (Matroska) is cut at the last frame by
// -sseof.
func TestThumbnail_ASecondPastTheEndTakesTheLastFrameOfAMatroskaVideo(t *testing.T) {
	tools := lookOrSkip(t)
	src := makeChangingVideo(t, tools, "v.mkv", 0)
	last := thumbnailBytes(t, tools, src, 0.85)
	require.NotEqual(t, thumbnailBytes(t, tools, src, 0), last)

	assert.Equal(t, last, thumbnailBytes(t, tools, src, 1000))
}
