// Package timing gives the local models of an estimate the time a result needs (Issue #1002), as the history of the results tells: the median of
// what the same model took for the same aspect ratio, resolution, length and speed mode. A model without such a result gets Unknown, never a
// guess. It is a package of its own so that package gentest keeps its size.
package timing

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
)

// The basis of the time of a model (Line.TimeBasis).
const (
	History = "history" // the median of what the same model took for the same settings before
	Unknown = "unknown" // no such result yet: nothing is guessed
)

// samples is how many of the latest succeeded results of a model are looked through for ones with the same settings.
const samples = 50

// unknownNote is what the estimate says when there is no result to go by.
const unknownNote = "このモデルをこの設定（解像度・長さ・速さのモード）で動かした実績がないため、所要時間は不明です"

// Line is the time of one model of an estimate (embedded in its line). A model that is not local has none of it.
type Line struct {
	TimeBasis      string `json:"time_basis,omitempty" jsonschema:"ローカルモデルの所要時間の根拠: history（同じ設定の過去の実績の中央値）/ unknown（実績なし。推測しない）"`
	SecondsPerItem *int   `json:"seconds_per_item,omitempty" jsonschema:"1 本あたりの所要時間の見込み（秒）。実績がないときは省略"`
	TimeSamples    int    `json:"time_samples,omitempty" jsonschema:"seconds_per_item の元にした実績の数"`
	TimeNote       string `json:"time_note,omitempty" jsonschema:"所要時間が不明の理由"`
}

// Total is the time of the batch (embedded in the estimate): the models that have a time, one result after the other (results that run
// side by side take less). It is a lower bound while UnknownTimeModels is not empty.
type Total struct {
	TotalSeconds      *int     `json:"total_seconds,omitempty" jsonschema:"ローカルモデルの所要時間の合計の見込み（秒）。実績のあるモデルだけを直列で足した目安で、unknown_time_models があれば下限。1 つも無いときは省略"`
	UnknownTimeModels []string `json:"unknown_time_models" jsonschema:"所要時間の実績がないローカルモデル（見込みなし）"`
}

// Plan is a local model of the estimate: where its line is (Index), how many results it makes and what is asked of it.
type Plan struct {
	Index  int
	Model  string
	Items  int
	Params record.ResultParams
}

// Fill returns the Line of every plan by its Index, and the Total.
func Fill(ctx context.Context, repo *gentestrepo.Repository, media string, plans []Plan) (map[int]Line, Total, error) {
	lines, total, seconds, known := map[int]Line{}, Total{UnknownTimeModels: []string{}}, 0, false
	for _, p := range plans {
		past, err := repo.RunDurations(ctx, p.Model, media, samples)
		if err != nil {
			return nil, Total{}, err
		}
		took := tookFor(past, p.Params)
		if len(took) == 0 {
			lines[p.Index] = Line{TimeBasis: Unknown, TimeNote: unknownNote}
			total.UnknownTimeModels = append(total.UnknownTimeModels, p.Model)
			continue
		}
		each := int(median(took).Round(time.Second) / time.Second)
		lines[p.Index] = Line{TimeBasis: History, SecondsPerItem: &each, TimeSamples: len(took)}
		seconds, known = seconds+each*p.Items, true
	}
	if known {
		total.TotalSeconds = &seconds
	}
	return lines, total, nil
}

// tookFor is how long the past results with the same settings took.
func tookFor(past []gentestrepo.RunDuration, want record.ResultParams) []time.Duration {
	var out []time.Duration
	for _, d := range past {
		var got record.ResultParams
		if json.Unmarshal(d.Params, &got) != nil {
			continue
		}
		if got.AspectRatio == want.AspectRatio && got.Resolution == want.Resolution && got.DurationSeconds == want.DurationSeconds && got.SpeedMode == want.SpeedMode {
			out = append(out, d.Took)
		}
	}
	return out
}

func median(d []time.Duration) time.Duration {
	d = slices.Clone(d)
	slices.Sort(d)
	if n := len(d); n%2 == 0 {
		return (d[n/2-1] + d[n/2]) / 2
	}
	return d[len(d)/2]
}
