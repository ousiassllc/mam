package media

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
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
		{"never cuts the text, whatever the number of lines", "あいうえおかきくけこさしすせそた", 5, "あいうえお\nかきくけこ\nさしすせそ\nた"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, wrapCaption(tc.text, tc.width))
		})
	}
}

// TestLayoutCaption_DrawsAMaxLengthCaptionWhole is the regression test of Issue #688: a caption of
// domain.MaxCaptionRunes characters was cut with an ellipsis on the vertical and square pictures.
func TestLayoutCaption_DrawsAMaxLengthCaptionWhole(t *testing.T) {
	texts := map[string]string{
		"wide":   strings.Repeat("あ", domain.MaxCaptionRunes),
		"spaced": strings.TrimSpace(strings.Repeat("word ", domain.MaxCaptionRunes/5)),
		"words":  strings.TrimSpace(strings.Repeat("abcdefghi ", domain.MaxCaptionRunes/10)),
	}
	sizes := []struct {
		name string
		w, h int
	}{
		{"16:9", 1920, 1080}, {"9:16", 1080, 1920}, {"1:1", 1080, 1080}, {"4:5", 1080, 1350},
		{"9:16 720p", 720, 1280}, {"9:16 480p", 480, 854}, {"16:9 draft", 320, 180},
	}
	for tn, text := range texts {
		for _, sz := range sizes {
			t.Run(tn+" "+sz.name, func(t *testing.T) {
				size, wrapped := layoutCaption(text, sz.w, sz.h)

				assert.NotContains(t, wrapped, "…")
				assert.Equal(t, noSpace(text), noSpace(wrapped), "no character is lost")
				assert.LessOrEqual(t, strings.Count(wrapped, "\n")+1, captionMaxLines)
				for _, line := range strings.Split(wrapped, "\n") {
					assert.LessOrEqual(t, len([]rune(line))*size, int(float64(sz.w)*captionWidthShare), "a line fits the picture")
				}
			})
		}
	}
}

// noSpace is s without any whitespace: where a caption was broken does not matter.
func noSpace(s string) string { return strings.Join(strings.Fields(s), "") }

func TestLayoutCaption_KeepsTheShortSideSizeWhenTheTextFits(t *testing.T) {
	size, wrapped := layoutCaption("朝が来た", 1080, 1920)

	assert.Equal(t, 72, size)
	assert.Equal(t, "朝が来た", wrapped)
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
