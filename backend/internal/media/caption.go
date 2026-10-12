package media

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Caption layout (F-18 Advanced: Caption). The text is drawn white with a dark box at the bottom
// of the picture; its size follows the shorter side, so a vertical and a horizontal video read
// the same. A caption that would take more than captionMaxLines lines at that size is drawn smaller
// instead of being cut: every caption the editor accepts is drawn whole.
const (
	captionSizeDivisor = 15  // font size = shorter side / 15
	captionWidthShare  = 0.9 // a line takes at most this much of the width
	captionMaxLines    = 3
	captionMinSize     = 12
)

// captionFilter is the drawtext filter that writes the caption of cut i, or "" when it has none.
// The text goes through a file in dir and is not expanded (expansion=none), so that no character of it
// (a % or a quote) needs escaping or is taken for markup.
func captionFilter(dir string, i int, text string, w, h int) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}
	size, wrapped := layoutCaption(text, w, h)
	path := filepath.Join(dir, fmt.Sprintf("caption-%d.txt", i))
	if err := os.WriteFile(path, []byte(wrapped), 0o600); err != nil {
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

// layoutCaption is the font size and the wrapped text of a caption on a w x h picture: the size
// follows the shorter side, and shrinks (down to captionMinSize) until the text fits in
// captionMaxLines lines. Below that size the text takes more lines rather than being cut.
func layoutCaption(text string, w, h int) (size int, wrapped string) {
	size = max(min(w, h)/captionSizeDivisor, captionMinSize)
	for ; ; size-- {
		wrapped = wrapCaption(text, max(int(float64(w)*captionWidthShare)/size, 4))
		if size <= captionMinSize || strings.Count(wrapped, "\n") < captionMaxLines {
			return size, wrapped
		}
	}
}

// wrapCaption breaks text into lines of at most width characters (a wide character counts as one),
// at a space when there is one in the last half of the line.
func wrapCaption(text string, width int) string {
	rs := []rune(text)
	var lines []string
	for len(rs) > 0 {
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
	return strings.Join(lines, "\n")
}
