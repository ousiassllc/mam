package videoenhance

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/cloudenhance"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/venhance"
)

// Request is one enhancement: the succeeded video result to make larger (Op upscale: Scale or TargetShortSide) or smoother (Op interpolate: Multiplier or
// TargetFPS), and the model. Of the values only the ones of Op count, and exactly one of the pair (venhance.Resolve).
type Request struct {
	ResultID string
	Op       cloudenhance.Op
	// Model is an id of Enhancers. "": the first local model that can run now (never one that costs).
	Model           string
	Scale           float64
	TargetShortSide int
	Multiplier      int
	TargetFPS       float64
	// EstimateID and ConfirmedCredits are the quote of a model of the cloud and the credits the user confirmed (equal to the quote); a local model takes none.
	EstimateID       string
	ConfirmedCredits float64
	// AdultConfirmed is the user's answer to venhance.AdultWarning, which a quote of an adult result carries: a model of the cloud is not started for an
	// adult source without it.
	AdultConfirmed bool
}

func (r Request) params() cloudenhance.Params {
	return cloudenhance.Params{Op: r.Op, Scale: r.Scale, TargetShortSide: r.TargetShortSide, Multiplier: r.Multiplier, TargetFPS: r.TargetFPS}
}

func invalid(format string, args ...any) error { return gterr.Invalid(format, args...) }

// target is what a request works on: the source result and its facts, the model and what is made.
type target struct {
	res   gentestrepo.Result
	facts cloudenhance.Source
	model modelcatalog.VideoEnhancer
	plan  venhance.Plan
	// adult says the source came from an adult model.
	adult bool
}

// prepare reads the source (a succeeded video result with its file: the facts are those ffprobe reads from the file, never what was asked of
// its generation), picks the model and works out the output. A PC without ffmpeg is refused with how to fix it.
func prepare(ctx context.Context, g *gentest.Service, r Request) (target, error) {
	if g.Repo == nil {
		return target{}, gterr.ErrUnavailable
	}
	res, err := g.Repo.GetResult(ctx, r.ResultID)
	if errors.Is(err, repository.ErrNotFound) {
		return target{}, gterr.ErrNotFound
	}
	if err != nil {
		return target{}, err
	}
	if res.Media != gentestrepo.MediaVideo || res.Status != gentestrepo.StatusSucceeded || res.FilePath == "" {
		return target{}, invalid("結果 %q は成功した動画ではありません（動画の結果だけ、大きくしたりコマを増やしたりできます）", r.ResultID)
	}
	model, err := pick(Enhancers(ctx, g), r.Op, r.Model)
	if err != nil {
		return target{}, err
	}
	file, err := g.ResultFile(ctx, r.ResultID)
	if err != nil {
		return target{}, err
	}
	facts, err := probe(ctx, g, file.Path)
	if err != nil {
		return target{}, err
	}
	plan, err := venhance.Resolve(facts, r.params(), !model.Paid, model.MaxOutputPixels)
	if err != nil {
		return target{}, err
	}
	return target{res: res, facts: facts, model: model, plan: plan, adult: adultSource(g, res)}, nil
}

// adultSource says a result was made by an adult model of the model manager's catalog, or is the result of an enhancement of one (the flag is passed on).
func adultSource(g *gentest.Service, res gentestrepo.Result) bool {
	var p record.ResultParams
	if json.Unmarshal(res.Params, &p) == nil && p.VideoEnhance != nil {
		return p.VideoEnhance.Adult
	}
	if g.CatalogInfo == nil {
		return false
	}
	info, ok := g.CatalogInfo(res.Model)
	return ok && info.Adult
}

// probe reads the facts of the video file. Without ffprobe (no ffmpeg found) it is the common error ffmpeg_missing: where to set ffmpeg.
func probe(ctx context.Context, g *gentest.Service, path string) (cloudenhance.Source, error) {
	if g.Probe == nil {
		return cloudenhance.Source{}, apperr.New(apperr.CodeFFmpegMissing)
	}
	st, err := os.Stat(path)
	if err != nil {
		return cloudenhance.Source{}, invalid("元の動画のファイルが見つかりません（履歴から消えたか、壊れています）")
	}
	info, err := g.Probe.Probe(ctx, path)
	if err != nil {
		return cloudenhance.Source{}, invalid("元の動画を調べられませんでした: %v", err)
	}
	return venhance.SourceOf(info, strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."), st.Size()), nil
}
