package media

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Caption layout (F-18 Advanced: Caption). The text is drawn white with a dark box at the bottom
// of the picture; its size follows the shorter side, so a vertical and a horizontal video read
// the same.
const (
	captionSizeDivisor = 15  // font size = shorter side / 15
	captionWidthShare  = 0.9 // a line takes at most this much of the width
	captionMaxLines    = 3
)

// captionFilter is the drawtext filter that writes the caption of cut i, or "" when it has none.
// The text goes through a file in dir and is not expanded (expansion=none), so that no character of it
// (a % or a quote) needs escaping or is taken for markup.
func captionFilter(dir string, i int, text string, w, h int) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}
	size := max(min(w, h)/captionSizeDivisor, 12)
	path := filepath.Join(dir, fmt.Sprintf("caption-%d.txt", i))
	if err := os.WriteFile(path, []byte(wrapCaption(text, max(int(float64(w)*captionWidthShare)/size, 4))), 0o600); err != nil {
		return "", fmt.Errorf("write caption: %w", err)
	}
	return fmt.Sprintf(",drawtext=textfile='%s':expansion=none:fontsize=%d:fontcolor=white:borderw=2:bordercolor=black@0.8:box=1:boxcolor=black@0.4:boxborderw=%d:line_spacing=%d:x=(w-text_w)/2:y=h-text_h-%d",
		filterPath(path), size, size/4, size/6, h/12), nil
}

// filterPath escapes a path for a quoted filter option value.
func filterPath(p string) string {
	p = filepath.ToSlash(p)
	p = strings.ReplaceAll(p, `\`, `\\`)
	p = strings.ReplaceAll(p, `'`, `'\''`)
	return strings.ReplaceAll(p, `:`, `\:`)
}

// wrapCaption breaks text into lines of at most width characters (a wide character counts as one),
// at a space when there is one in the last half of the line, and at most captionMaxLines lines; the
// rest is cut off with an ellipsis.
func wrapCaption(text string, width int) string {
	rs := []rune(text)
	var lines []string
	for len(rs) > 0 && len(lines) < captionMaxLines {
		if len(rs) <= width {
			lines, rs = append(lines, string(rs)), nil
			break
		}
		cut := width
		for j := width; j > width/2; j-- {
			if rs[j] == ' ' {
				cut = j
				break
			}
		}
		lines = append(lines, strings.TrimSpace(string(rs[:cut])))
		rs = []rune(strings.TrimSpace(string(rs[cut:])))
	}
	if len(rs) > 0 {
		last := []rune(lines[len(lines)-1])
		if utf8.RuneCountInString(string(last)) >= width {
			last = last[:width-1]
		}
		lines[len(lines)-1] = string(last) + "…"
	}
	return strings.Join(lines, "\n")
}
