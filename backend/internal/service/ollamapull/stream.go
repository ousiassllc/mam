package ollamapull

import (
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/track"
)

// line is one line of the NDJSON that POST /api/pull streams (docs/api.md of Ollama): the step as a status, the layer it is on (digest) with its total and completed bytes, or an error.
// The total and completed bytes are those of the layer: they begin again at 0 for the next one, and the answer has no total of the whole pull.
type line struct {
	Status    string `json:"status"`
	Digest    string `json:"digest"`
	Total     int64  `json:"total"`
	Completed int64  `json:"completed"`
	Error     string `json:"error"`
}

// progress puts the lines of a pull together as one progress of the whole. The whole is the total of the record of the catalog, because the stream has none: what is done is
// the layers that are finished (their whole size) and the part of the layers in progress (completed), and it never goes back when the next layer begins.
type progress struct {
	whole int64
	// layers are the bytes of each layer seen so far, by digest: the most it reported, and all of it once it reached its total.
	layers map[string]int64
	high   int
	update track.Update
}

func newProgress(whole int64) *progress { return &progress{whole: whole, layers: map[string]int64{}} }

// add reads a line and returns the progress after it. success says the stream ended well (100%).
func (p *progress) add(l line) (u track.Update, success bool) {
	switch {
	case l.Status == "success":
		p.high = 100
		p.update = track.Update{Percent: 100, Phase: "done", Message: "取得しました", DoneBytes: p.whole, TotalBytes: p.whole}
		return p.update, true
	case l.Digest != "" && l.Total > 0 && strings.HasPrefix(l.Status, "pulling"):
		done := min(l.Completed, l.Total)
		if p.layers[l.Digest] < done {
			p.layers[l.Digest] = done
		}
		sum := int64(0)
		for _, n := range p.layers {
			sum += n
		}
		sum = min(sum, p.whole)
		pct := 0
		if p.whole > 0 {
			pct = int(sum * 100 / p.whole) //nolint:gosec // bounded by 100
		}
		p.high = max(p.high, min(pct, 99)) // 100 is the success line: the check and the manifest come after the data
		p.update = track.Update{
			Percent: p.high, Phase: "pulling", DoneBytes: sum, TotalBytes: p.whole,
			Message: fmt.Sprintf("データを取得しています（%d%%・%s / %s）", p.high, fetch.HumanBytes(uint64(sum)), fetch.HumanBytes(uint64(p.whole))), //nolint:gosec // sizes
		}
	case strings.HasPrefix(l.Status, "pulling manifest"):
		p.update = track.Update{Percent: p.high, Phase: "manifest", Message: "manifest を取得しています"}
	case strings.HasPrefix(l.Status, "verifying"):
		p.update = track.Update{Percent: p.high, Phase: "verifying", Message: "検証しています", DoneBytes: p.update.DoneBytes, TotalBytes: p.update.TotalBytes}
	case strings.HasPrefix(l.Status, "writing manifest"):
		p.update = track.Update{Percent: p.high, Phase: "verifying", Message: "manifest を書いています", DoneBytes: p.update.DoneBytes, TotalBytes: p.update.TotalBytes}
	default:
		return p.update, false // a step that changes nothing the screen shows ("removing any unused layers")
	}
	return p.update, false
}
