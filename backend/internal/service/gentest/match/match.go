// Package match is the prompt match of the generation test (#916): from one reference image the Reasoning LLM writes an English
// prompt, a model generates with it, the LLM measures how much the picture looks like the reference and, if it does not look
// enough like it, fixes the prompt from the differences (and the words of the user) and generates again, until the target similarity
// is reached or the limit of rounds is.
//
// A match is a batch of the generation test (package gentest): every round is one of its results, so the pictures, their cost, the
// cancel and the history are those of a batch. The rounds are planned as queued results with no prompt; the job of the match
// (the job kind of the generation test, taken over by Handle) writes the prompt of a round when the round starts and removes the rounds
// it did not need. What a round learned (score, differences, what was fixed) is in the params of its result (package matchinfo).
//
// Like a batch it is estimated first (Estimate: the most rounds times the price of one picture, plus the calls of the LLM that have a
// price per token), and
// run only with the estimate the caller confirmed (Run), and a match that is over can go on (Request.ContinueBatchID): one more
// round with words of the user ("もう少し明るく"), or more rounds on its own. The pictures are made from the prompt alone: the
// reference image is shown to the LLM only, never given to the model, or the match would measure nothing.
package match

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/imagematch"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/matchinfo"
)

// The ways to go on (Request.Mode).
const (
	// ModeAuto repeats until the target similarity is reached or the rounds are used up.
	ModeAuto = "auto"
	// ModeOnce makes one picture from the prompt written for the reference and stops.
	ModeOnce = "once"
	// ModeAdjust makes one round at a time: the user looks at the result and asks for the next one, in words if they like.
	ModeAdjust = "adjust"
)

// Limits and defaults of a request.
const (
	DefaultTarget = 80
	DefaultRounds = 5
	// MaxRounds is the most rounds of one run.
	MaxRounds = 10
	// MaxInstructionRunes is the longest instruction of the user.
	MaxInstructionRunes = 1000
	// LLMLine is the Model of the line of the estimate that is the LLM writing and fixing the prompts, CompareLine the one that is the
	// LLM comparing the pictures (a line has no model of generation).
	LLMLine     = "match_llm"
	CompareLine = "match_llm_compare"
)

// Why a match that is over stopped (View.Stop).
const (
	StopReached   = "reached"   // the target similarity was reached
	StopLimit     = "limit"     // the rounds were used up
	StopCancelled = "cancelled" // the user cancelled it
	StopError     = "error"     // a round failed: its reason is in View.Error
)

// Deps are what the Service needs.
type Deps struct {
	Gentest *gentest.Service
	// Matcher is the LLM work (nil: a match is refused as unavailable).
	Matcher *imagematch.Matcher
}

// Service is the prompt match.
type Service struct {
	g *gentest.Service
	m *imagematch.Matcher

	mu      sync.Mutex
	pending map[string]string // batch id -> what the user said while the match runs, for the next fix
}

// New returns the Service and makes it the runner of the matches of d.Gentest (gentest.Deps.Extra).
func New(d Deps) *Service {
	s := &Service{g: d.Gentest, m: d.Matcher, pending: map[string]string{}}
	d.Gentest.Extra = s
	return s
}

// From returns the prompt match of the generation test service (nil when it has none).
func From(g *gentest.Service) *Service {
	if g == nil {
		return nil
	}
	s, _ := g.Extra.(*Service)
	return s
}

// batchParams is gentest_batch.params of a match: the keys the generation test reads and the marker of a match.
type batchParams struct {
	AspectRatio string `json:"aspect_ratio"`
	Count       int    `json:"count"`
	Match       *struct {
		Target int `json:"target_score"`
	} `json:"match,omitempty"`
}

// resultParams is gentest_result.params of a round: what the generation test reads (the aspect ratio) and the round.
type resultParams struct {
	AspectRatio string           `json:"aspect_ratio"`
	Match       *matchinfo.Round `json:"match,omitempty"`
}

func decodeBatch(b gentestrepo.Batch) (batchParams, bool) {
	var p batchParams
	_ = json.Unmarshal(b.Params, &p)
	return p, p.Match != nil
}

func decodeRound(r gentestrepo.Result) (matchinfo.Round, string) {
	var p resultParams
	_ = json.Unmarshal(r.Params, &p)
	if p.Match == nil {
		return matchinfo.Round{Round: r.Position + 1}, p.AspectRatio
	}
	return *p.Match, p.AspectRatio
}

// Handle runs the job of a match batch; handled is false for a batch that is not a match.
func (s *Service) Handle(ctx context.Context, _ domain.Job, batch gentestrepo.Batch) (handled bool, err error) {
	if _, ok := decodeBatch(batch); !ok {
		return false, nil
	}
	return true, s.loop(ctx, batch)
}

// Instruct adds words of the user to a match that is running: the next fix takes them in. A match that is over is a
// *gterr.ConflictError (continue it instead).
func (s *Service) Instruct(ctx context.Context, batchID, instruction string) error {
	instruction, err := cleanInstruction(instruction)
	if err != nil {
		return err
	}
	b, err := s.g.Repo.GetBatch(ctx, batchID)
	if err != nil {
		return gterr.NotFound(err)
	}
	if _, ok := decodeBatch(b); !ok {
		return gterr.Invalid("このバッチはプロンプトマッチではありません")
	}
	if b.FinishedAt != nil {
		return gterr.Conflict("このマッチはすでに終わっています。続けるときは「続ける」から指示を付けて実行してください")
	}
	if instruction == "" {
		return gterr.Invalid("指示を入力してください")
	}
	s.mu.Lock()
	s.pending[batchID] = joinWords(s.pending[batchID], instruction)
	s.mu.Unlock()
	return nil
}

func (s *Service) takePending(batchID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.pending[batchID]
	delete(s.pending, batchID)
	return w
}

func joinWords(a, b string) string {
	if a == "" || b == "" {
		return a + b
	}
	return a + "\n" + b
}
