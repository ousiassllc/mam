package upstream

import (
	"fmt"
	"io"
	"strings"
)

// Summary counts the results by how far they were verified; Failed is the number of pins with a problem.
type Summary struct {
	Total, Failed                int
	SHA256, Size, Alive, Skipped int
}

// Summarize counts the results.
func Summarize(results []Result) Summary {
	s := Summary{Total: len(results)}
	for _, r := range results {
		switch {
		case len(r.Problems) > 0:
			s.Failed++
		case r.Level == LevelSHA256:
			s.SHA256++
		case r.Level == LevelSkipped:
			s.Skipped++
		case r.Level == LevelSize:
			s.Size++
		default:
			s.Alive++
		}
	}
	return s
}

// WriteReport writes the problems (and, with verbose, the pins not proved by SHA-256) (every pin that no longer holds, with its catalog place and URL) and a summary that says how far the
// rest was verified. Without a download of the whole file, a pin whose level is "size" or "alive" is not proved to have its SHA-256.
func WriteReport(w io.Writer, results []Result, verbose bool) Summary {
	for _, r := range results {
		if len(r.Problems) == 0 {
			if verbose && r.Level != LevelSHA256 {
				fmt.Fprintf(w, "%-7s %s\n        %s\n", r.Level, r.Pin.Label, r.Pin.URL)
			}
			continue
		}
		fmt.Fprintf(w, "FAIL %s\n     %s\n", r.Pin.Label, r.Pin.URL)
		for _, p := range r.Problems {
			fmt.Fprintf(w, "     - %s\n", p)
		}
	}
	s := Summarize(results)
	fmt.Fprintf(w, "%s\n", strings.Repeat("-", 60))
	fmt.Fprintf(w, "pins: %d  failed: %d  verified by sha256: %d  by size only: %d  alive only: %d  skipped (gated, no HF_TOKEN): %d\n",
		s.Total, s.Failed, s.SHA256, s.Size, s.Alive, s.Skipped)
	return s
}
