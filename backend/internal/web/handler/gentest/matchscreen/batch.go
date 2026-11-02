package matchscreen

import (
	"context"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/match"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gtshare"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// stopText says why a match that is over stopped.
func stopText(v match.View) (text, tone string) {
	switch v.Stop {
	case match.StopReached:
		return "目標に届きました。ベストのプロンプトをそのまま使えます", "succeeded"
	case match.StopLimit:
		return "上限の回数まで試しました。いちばん似ていた回のプロンプトを使えます", "limit"
	case match.StopCancelled:
		return "取り消しました", "failed"
	}
	return "止まりました: " + v.Error, "failed"
}

func percentText(n int) string { return fmt.Sprintf("%d%%", n) }

// batchView is the match as the timeline draws it: the best similarity against the target and a card per round.
func batchView(v match.View) view.MatchBatch {
	b := view.MatchBatch{ID: v.BatchID, Finished: v.Finished, Target: v.Target, Done: v.Done, Total: v.Total, BestPrompt: v.BestPrompt}
	if v.Total > 0 {
		b.Percent = v.Done * 100 / v.Total
	}
	if v.Model != "" {
		b.Model, b.Answer = v.ModelLabel, commercial.Of(v.Model)
	}
	if v.ReferenceID != "" {
		b.Reference = paths.GenerationTestPart("inputs", v.ReferenceID, "file")
	}
	if v.EstimatedUSD > 0 {
		b.Estimate = gtshare.USD(v.EstimatedUSD)
	}
	if v.BestScore != nil {
		b.Best = percentText(*v.BestScore)
	}
	b.StateText, b.StateTone = "生成中", "running"
	if v.Finished {
		b.StopText, b.StateTone = stopText(v)
		b.StateText = map[string]string{"succeeded": "目標に届きました", "limit": "上限まで", "failed": "止まりました"}[b.StateTone]
		if v.Stop == match.StopCancelled {
			b.StateText = "取り消しました"
		}
	}
	for _, r := range v.Rounds {
		round := view.MatchRound{
			N: r.Round, ID: r.ResultID, Status: r.Status, StatusText: gtshare.StatusText[r.Status], Best: r.Best, Change: r.Change, Instruction: r.Instruction,
			Differences: r.Differences, Prompt: r.Prompt, BasePrompt: r.BasePrompt, BasedOn: r.BasedOn, Error: r.Error,
			Active:    !v.Finished && (r.Status == gentest.StatusRunning || r.Status == gentest.StatusQueued || (r.HasFile && r.Score == nil && r.Error == "")),
			DetailURL: paths.GenerationTestPart("results", r.ResultID),
		}
		if r.Status == gentest.StatusQueued {
			round.DetailURL = "" // a round that has not started has no result to open
		}
		if r.HasFile {
			round.ImageURL = paths.GenerationTestPart("results", r.ResultID, "file")
			if r.Prompt != "" {
				round.UseURL = useURL(r.ResultID)
			}
		}
		if r.Score != nil {
			round.Score, round.Percent, round.Reached = percentText(*r.Score), *r.Score, *r.Score >= v.Target
			round.ScoreNote = scoringText(r.Scoring)
		}
		if round.Active && r.HasFile && r.Score == nil {
			round.StatusText = "比べています"
		}
		if r.Best {
			b.BestUseURL = useURL(r.ResultID)
		}
		b.Rounds = append(b.Rounds, round)
	}
	return b
}

// useURL opens the generation test with the prompt of a round, the model and the shape it was made with: the models of the comparison
// are then added next to it (GET /generation-test?from_result=).
func useURL(resultID string) string { return paths.GenerationTest() + "?from_result=" + resultID }

// DetailKVs are the lines the detail of a result adds when the result is a round of a prompt match: what it measured, what was
// fixed and what the user said. Nothing for any other result.
func DetailKVs(ctx context.Context, svc *gentest.Service, batchID, resultID string) []view.KV {
	m := match.From(svc)
	if m == nil {
		return nil
	}
	v, err := m.View(ctx, batchID)
	if err != nil {
		return nil
	}
	for _, r := range v.Rounds {
		if r.ResultID != resultID {
			continue
		}
		kv := []view.KV{{Key: "プロンプトマッチ", Value: fmt.Sprintf("第 %d 回（目標 %d%%）", r.Round, v.Target)}}
		add := func(key, value string) {
			if value != "" {
				kv = append(kv, view.KV{Key: key, Value: value})
			}
		}
		if r.Score != nil {
			add("似てる度", percentText(*r.Score))
			add("点の内訳", scoringText(r.Scoring))
		}
		add("残った違い", strings.Join(r.Differences, " / "))
		add("直したところ", r.Change)
		add("言葉の指示", r.Instruction)
		add("直す前のプロンプト", r.BasePrompt)
		add("測定", r.Error)
		return kv
	}
	return nil
}
