package modelroute

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNoUsableModel is what *NoUsableError matches: no model of the assignment can be used now, and none may be chosen in its place.
var ErrNoUsableModel = errors.New("modelroute: no usable model")

// NoUsableError is the refusal of a generation that has no model to run on. It is raised before a job is queued (a start) and again by a job that finds
// the models gone. It names the cause and the ways out: register a key, change the assignment, or one of the usable models that match the request.
type NoUsableError struct {
	// Media is "image" or "video"; Key is the image purpose or the Quality Policy.
	Media, Key string
	// Assigned are the models of the assignment (or the Video Model the user fixed) and why none can be used.
	Assigned []Problem
	// Explicit: the assignment is the user's, so no model was chosen in its place; Pinned: the operator fixed the file.
	Explicit, Pinned bool
	// Candidates are the usable models that match the request (suggestions when Explicit); Unmatched the usable ones that do not.
	Candidates []Candidate
	Unmatched  []Unmatched
}

// Is makes errors.Is(err, ErrNoUsableModel) hold.
func (e *NoUsableError) Is(target error) bool { return target == ErrNoUsableModel }

// Error is the cause, in Japanese. The way out is the remedy of apperr.CodeNoUsableModel, which generation.Failure adds.
func (e *NoUsableError) Error() string { return e.Cause() }

// CauseOf is the cause of the *NoUsableError err wraps ("" for any other error).
func CauseOf(err error) string {
	if nu := new(NoUsableError); errors.As(err, &nu) {
		return nu.Cause()
	}
	return ""
}

// role is what the generation needed a model for.
func (e *NoUsableError) role() string {
	if e.Media == "video" {
		return "動画（Quality Policy: " + e.Key + "）"
	}
	return "画像（用途: " + e.Key + "）"
}

// Cause is what is wrong, without the remedy.
func (e *NoUsableError) Cause() string {
	var b strings.Builder
	b.WriteString(e.role() + "に使えるモデルがありません")
	if len(e.Assigned) > 0 {
		parts := make([]string, len(e.Assigned))
		for i, p := range e.Assigned {
			parts[i] = string(p.Model) + "＝" + p.Message
		}
		b.WriteString("（割り当て: " + strings.Join(parts, "、") + "）")
	}
	b.WriteString("。")
	switch {
	case e.Pinned:
		b.WriteString("割り当ては起動オプションで固定されているため、別のモデルへは自動で替えません。")
	case e.Explicit:
		b.WriteString("割り当てを設定で変えてあるため、別のモデルへは自動で替えません。")
	}
	switch {
	case len(e.Candidates) > 0:
		b.WriteString("条件に合って今使えるモデル: " + names(e.Candidates) + "。")
	case len(e.Unmatched) > 0:
		parts := make([]string, 0, maxListed)
		for _, u := range e.Unmatched[:min(len(e.Unmatched), maxListed)] {
			parts = append(parts, fmt.Sprintf("%s＝%s", u.Model, u.Why))
		}
		b.WriteString("今使えるモデルはありますが、条件に合いません（" + strings.Join(parts, "、") + "）。")
	default:
		b.WriteString("条件に合って今使えるモデルもありません。")
	}
	return b.String()
}
