package media

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrapCaption(t *testing.T) {
	tests := []struct {
		name, text string
		width      int
		want       string
	}{
		{"fits", "朝が来た", 10, "朝が来た"},
		{"breaks a wide text at the width", "あいうえおかきくけこ", 5, "あいうえお\nかきくけこ"},
		{"prefers a space in the last half of the line", "one two three four", 10, "one two\nthree four"},
		{"cuts what does not fit in three lines", "あいうえおかきくけこさしすせそた", 5, "あいうえお\nかきくけこ\nさしすせ…"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, wrapCaption(tc.text, tc.width))
		})
	}
}

// bottomStrip is the raw picture of the lower third of the frame at the given second: where the
// caption is drawn.
func bottomStrip(t *testing.T, tools Tools, path, at string) []byte {
	t.Helper()
	out, err := exec.Command(tools.ffmpeg, "-v", "error", "-ss", at, "-i", path, "-frames:v", "1", "-vf", "crop=iw:ih/3:0:2*ih/3,format=gray", "-f", "rawvideo", "-").Output()
	require.NoError(t, err)
	return out
}

func TestRender_CaptionIsDrawnOverTheCutOnly(t *testing.T) {
	tools := lookOrSkip(t)
	src := makeVideo(t, tools, "a.mp4", 320, 180, 24, 4, false)
	plain := filepath.Join(t.TempDir(), "plain.mp4")
	captioned := filepath.Join(t.TempDir(), "captioned.mp4")
	opt := RenderOptions{Draft: true}

	_, err := tools.Render(context.Background(), []Cut{{Path: src, Start: 0, End: 2}, {Path: src, Start: 0, End: 2}}, opt, plain)
	require.NoError(t, err)
	info, err := tools.Render(context.Background(), []Cut{{Path: src, Start: 0, End: 2, Caption: "朝が来た: it's 100% 'ok'"}, {Path: src, Start: 0, End: 2}}, opt, captioned)

	require.NoError(t, err, "the text needs no escaping")
	assert.InDelta(t, 4, info.Duration, 0.2)
	withText := meanAbsDiff(bottomStrip(t, tools, plain, "1"), bottomStrip(t, tools, captioned, "1"))
	without := meanAbsDiff(bottomStrip(t, tools, plain, "3"), bottomStrip(t, tools, captioned, "3"))
	assert.Greater(t, withText, 3*without+0.5, "the first cut carries the caption, the second does not")
}

// meanAbsDiff is the mean absolute difference of two equally long raw gray pictures.
func meanAbsDiff(a, b []byte) float64 {
	sum := 0
	for i := range a {
		sum += max(int(a[i])-int(b[i]), int(b[i])-int(a[i]))
	}
	return float64(sum) / float64(len(a))
}
