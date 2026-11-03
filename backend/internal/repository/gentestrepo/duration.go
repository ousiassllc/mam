package gentestrepo

import (
	"context"
	"encoding/json"
	"time"
)

// RunDuration is how long one succeeded result took, with what was asked of the model (Result.Params).
type RunDuration struct {
	Params json.RawMessage
	Took   time.Duration
}

// RunDurations returns how long the latest succeeded results of the model took, newest first: the basis of the time the estimate of
// a batch gives. A result that has no start (it ran before started_at existed) or that took no time is left out.
func (r *Repository) RunDurations(ctx context.Context, model, media string, limit int) ([]RunDuration, error) {
	all, err := queryAll(ctx, r, scanRunDuration,
		`SELECT params, started_at, finished_at FROM gentest_result
		 WHERE model = ? AND media = ? AND status = 'succeeded' AND started_at IS NOT NULL AND finished_at IS NOT NULL
		 ORDER BY finished_at DESC LIMIT ?`, model, media, limit)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, d := range all {
		if d.Took > 0 {
			out = append(out, d)
		}
	}
	return out, nil
}

func scanRunDuration(row scanner) (RunDuration, error) {
	var params, started, finished string
	if err := row.Scan(&params, &started, &finished); err != nil {
		return RunDuration{}, err
	}
	from, err := parse(started)
	if err != nil {
		return RunDuration{}, err
	}
	to, err := parse(finished)
	if err != nil {
		return RunDuration{}, err
	}
	return RunDuration{Params: json.RawMessage(params), Took: to.Sub(from)}, nil
}
