// Package matchinfo holds what the history keeps of one round of a prompt match (#916): the result that a round made carries it in
// its params ("match"). It is a package of its own so that the generation test service (which shows it in a result) and the prompt
// match (package match, which writes it) share one type without importing each other.
package matchinfo

import "github.com/ousiassllc/mam/backend/internal/agent/imagematch"

// Round is one round of a prompt match: the prompt of the round, how much the picture it made looks like the reference, what was
// still different, and what was fixed to get the prompt from the one before.
type Round struct {
	// Round counts from 1 over the whole match (a match that is continued goes on counting).
	Round int `json:"round"`
	// Target is the similarity the match aims at (set on the rounds that were planned for it).
	Target int `json:"target,omitempty"`
	// Score is the similarity, 0 to 100; nil when it was not measured (yet, or the comparison failed: see Error).
	Score *int `json:"score,omitempty"`
	// Scoring says how Score came about when the face is mixed in (#964): the LLM's points, the face's points and the weight, or why the
	// score is the LLM's alone. Absent when the face is not mixed in.
	Scoring *imagematch.Scoring `json:"scoring,omitempty"`
	// Differences are what still differs from the reference, in Japanese, the most important first.
	Differences []string `json:"differences,omitempty"`
	// BasedOn is the round whose prompt was fixed to get this one (0 for the prompt written from the reference image) and BasePrompt
	// is that prompt, so that the prompt before and after the fix can be read side by side. Change says in Japanese what was fixed.
	BasedOn    int    `json:"based_on,omitempty"`
	BasePrompt string `json:"base_prompt,omitempty"`
	Change     string `json:"change,omitempty"`
	// Instruction is what the user said in words for this fix, in the words they used (Japanese or English).
	Instruction string `json:"instruction,omitempty"`
	// Error says why the round did not get a measured score or a fixed prompt.
	Error string `json:"error,omitempty"`
	// LLM are the LLM calls the round made (the write or the fix, then the comparison) with the tokens they used (Call.Measured says
	// whether the provider reported them or they are the estimate's assumption) and what they cost. A call whose answer was refused
	// (a score out of range, a prompt that did not change) is not in it.
	LLM []imagematch.Call `json:"llm,omitempty"`
}
