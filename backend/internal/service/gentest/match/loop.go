package match

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/agent/imagematch"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/llm/failmsg"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/matchinfo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/runner"
)

// played is a round that has a measured similarity.
type played struct {
	result gentestrepo.Result
	info   matchinfo.Round
}

// state is what the rounds of a run look at: the reference and what was played so far.
type state struct {
	batch  gentestrepo.Batch
	ref    imagematch.Image
	played []played
}

// best is the round that looks most like the reference (the latest of equals: it carries the most fixes); nil when none was measured.
func (st *state) best() *played {
	var b *played
	for i := range st.played {
		if b == nil || *st.played[i].info.Score >= *b.info.Score {
			b = &st.played[i]
		}
	}
	return b
}

// base is the round whose prompt is fixed next: the one the user is looking at (the last) when they said something about it, else the
// best one, so that a fix that made it worse is not built on.
func (st *state) base(words bool) *played {
	if words && len(st.played) > 0 {
		return &st.played[len(st.played)-1]
	}
	return st.best()
}

// loop is the job of a match: the rounds it was planned with, one after another, until one reaches the target, a round fails, the user
// cancels (the rounds that did not start were cancelled then) or the rounds are used up. The rounds it did not need are removed.
func (s *Service) loop(ctx context.Context, batch gentestrepo.Batch) (err error) {
	g := s.g
	defer func() {
		bg := context.WithoutCancel(ctx)
		if ctx.Err() != nil {
			_, _ = g.Repo.FailQueued(bg, batch.ID, runner.ReasonInterrupted, g.Now().UTC())
		} else if err != nil {
			_, _ = g.Repo.FailQueued(bg, batch.ID, "マッチを始められませんでした: "+err.Error(), g.Now().UTC())
		} else {
			_, _ = g.Repo.DeleteQueued(bg, batch.ID)
		}
		_ = g.Repo.FinishBatch(bg, batch.ID, g.Now().UTC())
		s.takePending(batch.ID)
	}()
	results, err := g.Repo.Results(ctx, batch.ID)
	if err != nil {
		return err
	}
	st := &state{batch: batch}
	if st.ref, err = s.reference(ctx, batch.ID); err != nil {
		return err
	}
	for _, r := range results {
		if info, _ := decodeRound(r); r.Status == gentestrepo.StatusSucceeded && info.Score != nil {
			st.played = append(st.played, played{r, info})
		}
	}
	for _, q := range results {
		if q.Status != gentestrepo.StatusQueued {
			continue
		}
		if ctx.Err() != nil || s.round(ctx, st, q) {
			return nil
		}
	}
	return nil
}

// reference reads the reference image of the match, ready for the LLM.
func (s *Service) reference(ctx context.Context, batchID string) (imagematch.Image, error) {
	inputs, err := s.g.Repo.Inputs(ctx, batchID)
	if err != nil || len(inputs) == 0 {
		return imagematch.Image{}, errors.Join(err, errors.New("the match has no reference image"))
	}
	if s.g.InputHidden(ctx, inputs[0]) { // the age gate was closed after the match was queued (a resumed one, a go-on): the picture is not read, not shown to the LLM
		return imagematch.Image{}, runner.ErrInputHidden
	}
	return s.picture(inputs[0].FilePath)
}

// picture reads a stored file (a path below the generation test's directory) for the LLM.
func (s *Service) picture(relative string) (imagematch.Image, error) {
	data, err := os.ReadFile(filepath.Join(s.g.DataDir, gentest.DirName, filepath.FromSlash(relative))) //nolint:gosec // a path the generation test wrote below its own directory
	if err != nil {
		return imagematch.Image{}, err
	}
	return imagematch.Prepare(data)
}

// round makes one planned round: it writes the prompt (from the reference, or fixing the base round), has the model generate, measures
// the similarity and records it. It reports true when the match stops here.
func (s *Service) round(ctx context.Context, st *state, q gentestrepo.Result) (stop bool) {
	g := s.g
	if cur, err := g.Repo.GetResult(ctx, q.ID); err != nil || cur.Status != gentestrepo.StatusQueued {
		return true // cancelled (or deleted) while the match was on its way
	}
	info, aspect := decodeRound(q)
	info.Instruction = joinWords(info.Instruction, s.takePending(st.batch.ID))
	prompt, err := s.write(ctx, st, &info)
	if err != nil {
		if len(info.LLM) > 0 {
			s.record(ctx, q.ID, aspect, info) // the call that was paid for stays in the cost of the match
		}
		s.failRound(ctx, q, err)
		return true
	}
	raw, _ := json.Marshal(resultParams{AspectRatio: aspect, Match: &info})
	if err := g.Repo.SetResultPrompt(ctx, q.ID, prompt, raw, g.Now().UTC()); err != nil {
		s.failRound(ctx, q, err)
		return true
	}
	made, err := g.Repo.GetResult(ctx, q.ID)
	if err != nil {
		return true
	}
	g.Generate(ctx, made)
	if made, err = g.Repo.GetResult(ctx, q.ID); err != nil || made.Status != gentestrepo.StatusSucceeded {
		return true // failed or cancelled: the result says so
	}
	img, err := s.picture(made.FilePath)
	var j imagematch.Judgement
	if err == nil {
		j, err = s.m.Compare(ctx, st.ref, img)
	}
	if err != nil {
		g.Logger.Warn("prompt match: measure the similarity failed", "result_id", q.ID, "err", err)
		info.Error = "似てる度を測れませんでした: " + failure(err)
		info.LLM = appendMade(info.LLM, j.Call)
		s.record(ctx, q.ID, aspect, info)
		return true
	}
	info.Score, info.Differences, info.Scoring = &j.Score, j.Differences, j.Scoring
	info.LLM = append(info.LLM, j.Call)
	s.record(ctx, q.ID, aspect, info)
	st.played = append(st.played, played{made, info})
	return j.Score >= cmp.Or(info.Target, DefaultTarget)
}

// write is the prompt of the round: written for the reference when nothing was measured yet, else the base round's prompt fixed from its
// differences and the words of the user. It fills what the round records about it in info.
func (s *Service) write(ctx context.Context, st *state, info *matchinfo.Round) (string, error) {
	base := st.base(info.Instruction != "")
	if base == nil {
		prompt, call, err := s.m.Describe(ctx, st.ref, info.Instruction)
		info.LLM = appendMade(info.LLM, call)
		return prompt, err
	}
	img, err := s.picture(base.result.FilePath)
	if err != nil {
		return "", err
	}
	rev, err := s.m.Revise(ctx, imagematch.ReviseInput{
		Reference: st.ref, Generated: img, Prompt: base.result.SentPrompt, Differences: base.info.Differences, Instruction: info.Instruction,
	})
	info.LLM = appendMade(info.LLM, rev.Call)
	if err != nil {
		return "", err
	}
	info.BasedOn, info.BasePrompt, info.Change = base.info.Round, base.result.SentPrompt, rev.Change
	return rev.Prompt, nil
}

// appendMade adds the call to the record of the round when the LLM answered it: an answer that could not be used was paid for all the same.
func appendMade(calls []imagematch.Call, c imagematch.Call) []imagematch.Call {
	if !c.Made() {
		return calls
	}
	return append(calls, c)
}

// record writes what a round learned into the params of its result.
func (s *Service) record(ctx context.Context, id, aspect string, info matchinfo.Round) {
	raw, _ := json.Marshal(resultParams{AspectRatio: aspect, Match: &info})
	if err := s.g.Repo.SetResultParams(context.WithoutCancel(ctx), id, raw); err != nil {
		s.g.Logger.Error("prompt match: record the round failed", "result_id", id, "err", err)
	}
}

// failRound settles a planned round that could not be made, with the reason in words.
func (s *Service) failRound(ctx context.Context, q gentestrepo.Result, err error) {
	s.g.Logger.Warn("prompt match: round failed", "result_id", q.ID, "err", err)
	st := gentestrepo.Settlement{Status: gentestrepo.StatusFailed, Error: failure(err), At: s.g.Now().UTC()}
	if ctx.Err() != nil {
		st.Error = runner.ReasonInterrupted
	}
	if err := s.g.Repo.Settle(context.WithoutCancel(ctx), q.ID, st); err != nil {
		s.g.Logger.Error("prompt match: settle the round failed", "result_id", q.ID, "err", err)
	}
}

// failure says in words why the LLM could not do its part.
func failure(err error) string {
	switch {
	case errors.Is(err, llm.ErrNoAPIKey):
		return failmsg.NoKeyMessage(err)
	case errors.Is(err, llm.ErrCircuitOpen):
		return "LLM が一時的に使えません。しばらく待ってからもう一度試してください"
	case errors.Is(err, llm.ErrTimeout):
		return "LLM の応答が時間内に返りませんでした"
	case errors.Is(err, imagematch.ErrNotRevised):
		return "プロンプトをこれ以上直せませんでした（LLM が同じプロンプトを返しました。指示を足すと直せることがあります）"
	case errors.Is(err, llm.ErrInvalidOutput):
		return "LLM の答えが使えない形でした"
	}
	var api *llm.APIError
	if errors.As(err, &api) {
		return "LLM の呼び出しに失敗しました"
	}
	return "処理できませんでした。mam のログを確認してください"
}
