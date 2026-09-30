package repair

import (
	"slices"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Situation is what the deterministic rules know when they narrow down the repair strategy.
type Situation struct {
	// Result is the Judge result being repaired.
	Result domain.JudgeResult
	// Tried are the strategies the Shot's repair history already carried out.
	Tried []domain.RepairAction
	// HasReferences is true when the Shot has a Reference Pack to reinforce.
	HasReferences bool
	// PromptLocked is true when the Shot's Prompt is LOCKed: its Shot Spec may not be rewritten.
	PromptLocked bool
	// OtherModel is true when Different Model has another video model to go to (and it fits the budget).
	OtherModel bool
}

// Candidates are the strategies that may be used, in the order of spec/spec.md §13. This is a
// deterministic filter; the Repair Agent (the Reasoning LLM) only chooses among what remains, so it
// can never pick a strategy that cannot be carried out.
//
//   - Prompt Repair and Partial Regeneration rewrite the Shot Spec: not with Prompt LOCKed.
//   - Reference Reinforcement needs References; Different Model needs another model.
//   - Edit-Level Repair keeps the Take and fixes it in the edit: only when every Issue is a time range
//     that can be cut, none is high severity and none breaks a LOCK.
//   - A strategy that was already tried on the Shot is not repeated (it did not work), except Full
//     Retake: a new generation of the same spec is a new draw.
//
// Full Retake is always a candidate, so the list is never empty.
func Candidates(s Situation) []domain.RepairAction {
	var out []domain.RepairAction
	for _, a := range domain.RepairActions() {
		switch {
		case a != domain.RepairFullRetake && slices.Contains(s.Tried, a):
			continue
		case (a == domain.RepairPrompt || a == domain.RepairPartialRegeneration) && s.PromptLocked:
			continue
		case a == domain.RepairReferenceReinforce && !s.HasReferences:
			continue
		case a == domain.RepairDifferentModel && !s.OtherModel:
			continue
		case a == domain.RepairEditLevel && !editable(s.Result):
			continue
		}
		out = append(out, a)
	}
	return out
}

// editable reports whether the Issues of the result can be fixed by editing the Take.
func editable(r domain.JudgeResult) bool {
	if len(r.Issues) == 0 || r.HasLockViolation() {
		return false
	}
	for _, is := range r.Issues {
		if is.Severity == domain.SeverityHigh || is.TimeRange == "" {
			return false
		}
	}
	return true
}

// Rule decides the strategy without asking a model when the rules leave no real choice:
//
//   - a single candidate is taken;
//   - a face_identity_drift Issue makes Reference Reinforcement the strategy (spec/spec.md §12.4).
//
// It returns false when the Repair Agent has to choose.
func Rule(s Situation, candidates []domain.RepairAction) (domain.RepairAction, string, bool) {
	if len(candidates) == 1 {
		return candidates[0], "the only strategy left", true
	}
	if slices.Contains(candidates, domain.RepairReferenceReinforce) {
		for _, is := range s.Result.Issues {
			if is.Type == domain.IssueTypeFaceIdentityDrift {
				return domain.RepairReferenceReinforce, "face identity drift: reference reinforcement is preferred", true
			}
		}
	}
	return "", "", false
}

// Fallback is the strategy used when the Repair Agent cannot answer (no key, rate limit, an answer
// that is not a candidate): the Judge's own recommendation when it is a candidate, else Full Retake.
func Fallback(s Situation, candidates []domain.RepairAction) (domain.RepairAction, string) {
	if rs := s.Result.RepairStrategy; rs != nil && slices.Contains(candidates, rs.Action) {
		return rs.Action, "the Repair Agent could not decide: the Judge's recommendation is used"
	}
	return domain.RepairFullRetake, "the Repair Agent could not decide: full retake"
}

// Scored is a judged Take reduced to what Beats compares.
type Scored struct {
	Score    float64
	HasScore bool
	Lock     bool // the result has a LOCK violation
	High     int  // the number of high-severity Issues
}

// Rank reduces a Judge result to a Scored under the Production Template.
func Rank(r domain.JudgeResult, t domain.ProductionTemplate) Scored {
	sc := Scored{Lock: r.HasLockViolation()}
	sc.Score, sc.HasScore = r.Score(t)
	for _, is := range r.Issues {
		if is.Severity == domain.SeverityHigh {
			sc.High++
		}
	}
	return sc
}

// Beats reports whether a is strictly a better Take to repair from than b: no LOCK violation first,
// then the higher overall score, then fewer high-severity Issues.
func (a Scored) Beats(b Scored) bool {
	switch {
	case a.Lock != b.Lock:
		return !a.Lock
	case a.HasScore != b.HasScore:
		return a.HasScore
	case a.HasScore && a.Score != b.Score:
		return a.Score > b.Score
	}
	return a.High < b.High
}
