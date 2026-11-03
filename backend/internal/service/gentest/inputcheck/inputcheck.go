// Package inputcheck is the check of the input images of a generation test request against the models that were chosen (#1009). A model never silently
// drops an input:
//
//   - the model needs an input the request lacks (an I2V model without a first frame)       -> the request is stopped (Missing: input_missing)
//   - none of the attached images is used by any chosen model (a picture for a T2V model)  -> the request is stopped (Unused: input_unused)
//   - some are not used by some model                                                       -> a warning (Plan.Notes), and the result records them (Plan.Unused)
//
// package gentest runs it as a step of its estimate (so the screen's estimate and the MCP tools make the same check) and, through Service.CheckInputs,
// for the notice of a form that is still being filled in. It works on the roles of the images ("reference", "first_frame", ...), not on the images.
package inputcheck

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/refs"
)

// The codes of a Note (the codes of package gentest's Warning).
const (
	WarnInputsDropped  = refs.WarnInputsDropped // an input image the model cannot use is left out for that model
	WarnTooMany        = refs.WarnTooMany       // more free references than the model takes
	WarnFramesOverFree = "frames_over_refs"     // first / last frames and free references cannot be used together
)

// RequiredReference is the name of the reference slot in Model.Requires (the first and last frame are named by their roles).
const RequiredReference = "reference"

// Note is a warning of a plan: an input image a model leaves out.
type Note struct{ Code, Message string }

// Unused is an input image (its index in the request's references) a model is not given, and why.
type Unused struct {
	Index  int
	Reason string
}

// Plan is how one model takes the input images: the indexes it is given, the ones it is not (with why), and the warnings about those.
type Plan struct {
	Used   []int
	Unused []Unused
	Notes  []Note
	// FirstFrame says the model is given a first frame.
	FirstFrame bool
}

// Model is what the check needs to know of a chosen model.
type Model struct {
	ID, Label string
	// Video: a video model. TakesImages: it takes input images at all (an image to video model, a model with reference images).
	Video, TakesImages bool
	// Requires are the inputs it cannot run without: "first_frame", "last_frame", RequiredReference.
	Requires []string
}

// UnusedOf lists the images 0..n-1 that are not in used, with the reason reasonOf gives for each.
func UnusedOf(n int, used []int, reasonOf func(index int) string) []Unused {
	var out []Unused
	for i := range n {
		if !slices.Contains(used, i) {
			out = append(out, Unused{Index: i, Reason: reasonOf(i)})
		}
	}
	return out
}

// ImageUnused lists the images an image model is not given: why is that it takes none, or only max of them.
func ImageUnused(n int, used []int, accepts bool, max int) []Unused {
	reason := "このモデルは参照画像を使えません"
	if accepts && max > 0 {
		reason = fmt.Sprintf("このモデルの参照画像は %d 枚までです", max)
	}
	return UnusedOf(n, used, func(int) string { return reason })
}

func indexesOf(roles []string, keep func(role string) bool) []int {
	var out []int
	for i, r := range roles {
		if keep(r) {
			out = append(out, i)
		}
	}
	return out
}

// Video decides which input images (by role) a video model is given. A model that takes no image at all (text to video) is given none, with the
// words that name what to do instead.
func Video(caps modeladapter.VideoCapabilities, label string, roles []string) Plan {
	var p Plan
	if len(roles) == 0 {
		return p
	}
	if !caps.FirstFrame && !caps.LastFrame && caps.MaxReferenceImages == 0 {
		p.Notes = []Note{{WarnInputsDropped, fmt.Sprintf("%s は文章だけから作るモデルなので、添えた画像は使われません。画像から作るなら I2V のモデルを選んでください", label)}}
		p.Unused = UnusedOf(len(roles), nil, func(int) string { return "文章だけから作るモデルなので、画像は使われません" })
		return p
	}
	has := func(role string) bool { return slices.Contains(roles, role) }
	first := caps.FirstFrame && has(gentestrepo.RoleFirstFrame)
	last := caps.LastFrame && has(gentestrepo.RoleLastFrame) && first
	reasons := map[int]string{}
	note := func(code, format string, args ...any) {
		p.Notes = append(p.Notes, Note{code, fmt.Sprintf(format, args...)})
	}
	drop := func(idx []int, reason string) {
		for _, i := range idx {
			reasons[i] = reason
		}
	}
	ofRole := func(role string) []int { return indexesOf(roles, func(r string) bool { return r == role }) }
	free := ofRole(gentestrepo.RoleReference)
	if len(free) > 0 && caps.MaxReferenceImages == 0 {
		note(WarnInputsDropped, "%s は自由参照の画像を使えないため、使いません", label)
		drop(free, "このモデルは自由参照の画像を使えません")
		free = nil
	}
	if len(free) > caps.MaxReferenceImages && caps.MaxReferenceImages > 0 {
		note(WarnTooMany, "%s の自由参照は %d 枚までのため、先頭の %d 枚だけ使います", label, caps.MaxReferenceImages, caps.MaxReferenceImages)
		drop(free[caps.MaxReferenceImages:], fmt.Sprintf("このモデルの自由参照は %d 枚までです", caps.MaxReferenceImages))
		free = free[:caps.MaxReferenceImages]
	}
	if (first || last) && len(free) > 0 {
		note(WarnFramesOverFree, "%s は最初・最後のフレームと自由参照を同時に使えないため、フレームを使います", label)
		drop(free, "このモデルは最初・最後のフレームと自由参照を同時に使えません（フレームを使います）")
		free = nil
	}
	if has(gentestrepo.RoleFirstFrame) && !caps.FirstFrame {
		note(WarnInputsDropped, "%s は最初のフレームを使えないため、使いません", label)
		drop(ofRole(gentestrepo.RoleFirstFrame), "このモデルは最初のフレームを使えません")
	}
	if has(gentestrepo.RoleLastFrame) && !last {
		note(WarnInputsDropped, "%s は最後のフレームを使えない（または最初のフレームが無い）ため、使いません", label)
		drop(ofRole(gentestrepo.RoleLastFrame), "このモデルは最後のフレームを使えません（または最初のフレームが無い）")
	}
	p.Used = indexesOf(roles, func(r string) bool {
		return r == gentestrepo.RoleFirstFrame && first || r == gentestrepo.RoleLastFrame && last
	})
	p.Used = append(p.Used, free...)
	slices.Sort(p.Used)
	p.Unused = UnusedOf(len(roles), p.Used, func(i int) string { return reasons[i] })
	p.FirstFrame = first
	return p
}

var (
	inputLabel = map[string]string{gentestrepo.RoleFirstFrame: "最初のフレーム", gentestrepo.RoleLastFrame: "最後のフレーム", RequiredReference: "参照画像"}
	// inputParam is the parameter of the workflow a required input fills (the word the adapter's 422 and errmap use).
	inputParam = map[string]string{gentestrepo.RoleFirstFrame: "first_frame", gentestrepo.RoleLastFrame: "last_frame", RequiredReference: "reference_image"}
)

// Missing is the error of a request that lacks an input the model cannot run without, nil when it has all of them. For a first or last frame the words
// say what the picture attached is (a "reference": the role to change), so that the fix is in the sentence. A reference is any picture of an image batch.
func Missing(video bool, roles []string, m Model) *apperr.Error {
	for _, need := range m.Requires {
		if slices.Contains(roles, need) || need == RequiredReference && !video && len(roles) > 0 {
			continue
		}
		what := inputLabel[need]
		cause := fmt.Sprintf("「%s」は%sの画像が必要です。", m.Label, what)
		switch {
		case need == RequiredReference:
			cause = fmt.Sprintf("「%s」は参照画像が必要です。画像を添えてください。", m.Label)
		case slices.Contains(roles, gentestrepo.RoleReference):
			cause += fmt.Sprintf("添えた画像が「参照」になっています。「%s」に変えてください。", what)
		default:
			cause += fmt.Sprintf("画像を添えて、役割を「%s」にしてください。", what)
		}
		return apperr.New(apperr.CodeInputMissing).With("param", inputParam[need]).WithCause(cause).
			WithDetail(fmt.Sprintf("%s needs %s, the request has none (%s)", m.ID, inputParam[need], summary(roles)))
	}
	return nil
}

// AllUnused is the error of a request whose attached images no chosen model uses (models and plans are in the same order); nil when there is no image or
// some model uses one.
func AllUnused(roles []string, models []Model, plans []Plan) *apperr.Error {
	if len(roles) == 0 || slices.ContainsFunc(plans, func(p Plan) bool { return len(p.Used) > 0 }) {
		return nil
	}
	cause := fmt.Sprintf("添えた画像（%d 枚）は、選んだモデルのどれでも使われません。", len(roles))
	if len(models) == 1 && !models[0].TakesImages {
		cause = fmt.Sprintf("「%s」は文章だけから作るモデルなので、添えた画像は使われません。", models[0].Label)
	} else {
		parts := make([]string, len(models))
		for i, m := range models {
			reason := "このモデルは添えた画像を使えません"
			if u := plans[i].Unused; len(u) > 0 && u[0].Reason != "" {
				reason = u[0].Reason
			}
			parts[i] = fmt.Sprintf("「%s」: %s", m.Label, reason)
		}
		cause += strings.Join(parts, "、") + "。"
	}
	return apperr.New(apperr.CodeInputUnused).WithCause(cause).WithDetail("no chosen model uses an input image (" + summary(roles) + ")")
}

func summary(roles []string) string {
	if len(roles) == 0 {
		return "no input image"
	}
	return "input roles: " + strings.Join(roles, ", ")
}
