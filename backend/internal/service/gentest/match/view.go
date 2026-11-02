package match

import (
	"cmp"
	"context"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/imagematch"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// Scoring is how the similarity of a round came about when the face is mixed in (#964), and the reasons the face can be left out.
type Scoring = imagematch.Scoring

// Why the face was left out of a score that was to have it (Scoring.Skipped).
const (
	SkipUnavailable     = imagematch.SkipUnavailable
	SkipReferenceNoFace = imagematch.SkipReferenceNoFace
	SkipGeneratedNoFace = imagematch.SkipGeneratedNoFace
	SkipError           = imagematch.SkipError
)

// Round is a round of a match as the screen and the MCP tools show it: what the round learned and the result that made the picture.
type Round struct {
	Round    int    `json:"round" jsonschema:"回の番号（続きでも通しで数える）"`
	ResultID string `json:"result_id" jsonschema:"この回の生成結果（get_generation_result に渡せる）"`
	Status   string `json:"status" jsonschema:"生成の状態: queued / running / succeeded / failed / cancelled"`
	// Prompt is the English prompt of the round ("" until it is written).
	Prompt  string `json:"prompt"`
	HasFile bool   `json:"has_file"`
	// Score is the similarity to the reference, 0 to 100 (absent until it is measured).
	Score *int `json:"score,omitempty"`
	// Scoring is how Score came about when the face is mixed in (#964): the LLM's points, the face's points and its weight, or why the
	// face was left out. Absent when the face is not mixed in.
	Scoring     *Scoring `json:"scoring,omitempty" jsonschema:"点の内訳（顔を混ぜているとき。llm = LLM の点、face = 顔の点・一致度・重み、face_skipped = 顔を使わなかった理由）"`
	Differences []string `json:"differences,omitempty" jsonschema:"まだ違うところ（日本語。重要な順）"`
	// BasedOn is the round whose prompt this one fixed (absent for the prompt written from the reference), BasePrompt that prompt.
	BasedOn     int    `json:"based_on,omitempty"`
	BasePrompt  string `json:"base_prompt,omitempty" jsonschema:"直す前のプロンプト"`
	Change      string `json:"change,omitempty" jsonschema:"直したところ（日本語）"`
	Instruction string `json:"instruction,omitempty" jsonschema:"この回で取り込んだ言葉の指示"`
	// Error is why the round has no picture or no score.
	Error string `json:"error,omitempty"`
	// Best marks the round that looks most like the reference.
	Best bool `json:"best,omitempty"`
}

// View is a match with its rounds.
type View struct {
	BatchID    string `json:"batch_id"`
	Model      string `json:"model"`
	ModelLabel string `json:"model_label"`
	// ReferenceID is the input of the batch that is the reference image (get_generation_result's inputs).
	ReferenceID string `json:"reference_input_id"`
	AspectRatio string `json:"aspect_ratio"`
	Target      int    `json:"target_score" jsonschema:"目標の似てる度（0〜100）"`
	// Finished says the match is over: nothing is running and nothing more will be made (until it is continued).
	Finished bool `json:"finished"`
	// Stop is why a match that is over stopped: reached / limit / cancelled / error. "" while it runs.
	Stop string `json:"stop,omitempty"`
	// Error is the reason of the last failure, when Stop is error.
	Error  string  `json:"error,omitempty"`
	Rounds []Round `json:"rounds"`
	// Total is the rounds planned, Done the ones that are over.
	Total int `json:"total"`
	Done  int `json:"done"`
	// Best is the number of the round that looks most like the reference (0: none was measured); BestScore its similarity and
	// BestPrompt its prompt: the one to use.
	Best         int       `json:"best_round"`
	BestScore    *int      `json:"best_score,omitempty"`
	BestPrompt   string    `json:"best_prompt,omitempty"`
	EstimatedUSD float64   `json:"estimated_usd"`
	CreatedAt    time.Time `json:"created_at"`
}

// IsMatch reports whether the batch is a prompt match and, if it is, whether it is still going (the generation test's own state says it
// is over as soon as the last picture is made, before the last similarity is measured). gterr.ErrNotFound for a batch that is not there.
func (s *Service) IsMatch(ctx context.Context, batchID string) (match, running bool, err error) {
	b, err := s.g.Repo.GetBatch(ctx, batchID)
	if err != nil {
		return false, false, gterr.NotFound(err)
	}
	_, match = decodeBatch(b)
	return match, match && b.FinishedAt == nil, nil
}

// View returns the match (gterr.ErrNotFound for a batch that is not there, *gterr.InvalidError for one that is not a match).
func (s *Service) View(ctx context.Context, batchID string) (View, error) {
	b, err := s.g.Repo.GetBatch(ctx, batchID)
	if err != nil {
		return View{}, gterr.NotFound(err)
	}
	bp, ok := decodeBatch(b)
	if !ok {
		return View{}, gterr.Invalid("このバッチはプロンプトマッチではありません")
	}
	results, err := s.g.Repo.Results(ctx, batchID)
	if err != nil {
		return View{}, err
	}
	v := View{
		BatchID: b.ID, AspectRatio: bp.AspectRatio, Target: bp.Match.Target, Finished: b.FinishedAt != nil, Rounds: []Round{},
		EstimatedUSD: b.EstimatedCost, CreatedAt: b.CreatedAt,
	}
	if inputs, err := s.g.Repo.Inputs(ctx, batchID); err == nil && len(inputs) > 0 {
		v.ReferenceID = inputs[0].ID
	}
	var lastFailure string
	cancelled := false
	for _, r := range results {
		info, _ := decodeRound(r)
		if v.Model == "" {
			v.Model, v.ModelLabel = r.Model, modelreg.Label(r.Model)
		}
		v.Rounds = append(v.Rounds, Round{
			Round: info.Round, ResultID: r.ID, Status: r.Status, Prompt: r.SentPrompt, HasFile: r.Status == gentestrepo.StatusSucceeded && r.FilePath != "",
			Score: info.Score, Scoring: info.Scoring, Differences: info.Differences, BasedOn: info.BasedOn, BasePrompt: info.BasePrompt, Change: info.Change,
			Instruction: info.Instruction, Error: cmp.Or(r.Error, info.Error),
		})
		over := r.Status == gentestrepo.StatusFailed || r.Status == gentestrepo.StatusCancelled || (info.Score != nil || info.Error != "")
		v.Done += btoi(over)
		switch { // how the match ended is how its latest round did: a later round that was measured makes an earlier failure history
		case r.Status == gentestrepo.StatusCancelled:
			cancelled = true
		case r.Status == gentestrepo.StatusFailed:
			cancelled, lastFailure = false, r.Error
		case info.Error != "":
			cancelled, lastFailure = false, info.Error
		case info.Score != nil:
			cancelled, lastFailure = false, ""
		}
	}
	v.Total = len(v.Rounds)
	v.Best = markBest(v.Rounds)
	if v.Best > 0 {
		best := v.Rounds[v.Best-1]
		v.BestScore, v.BestPrompt, v.Best = best.Score, best.Prompt, best.Round
	}
	if v.Finished {
		v.Stop, v.Error = stopOf(v, cancelled, lastFailure)
	}
	return v, nil
}

// markBest marks the round that looks most like the reference (the latest of equals) and returns its index + 1 (0: none).
func markBest(rounds []Round) int {
	best := -1
	for i, r := range rounds {
		if r.Score != nil && (best < 0 || *r.Score >= *rounds[best].Score) {
			best = i
		}
	}
	if best >= 0 {
		rounds[best].Best = true
	}
	return best + 1
}

func stopOf(v View, cancelled bool, failure string) (stop, errText string) {
	switch {
	case v.BestScore != nil && *v.BestScore >= v.Target:
		return StopReached, ""
	case cancelled:
		return StopCancelled, ""
	case failure != "":
		return StopError, failure
	}
	return StopLimit, ""
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
