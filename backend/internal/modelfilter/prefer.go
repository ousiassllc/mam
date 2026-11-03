package modelfilter

import (
	"slices"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
)

// roleInputs is the input a model needs for an input image of each role (the role of a generation test's input image: localcfg.Role* and the video
// roles first_frame / last_frame). "reference" is the whole picture, for an image model and for a video model alike.
var roleInputs = map[string]traits.Input{
	"reference": traits.ReferenceWhole, "face": traits.ReferenceFace, "style": traits.ReferenceStyle, "edit": traits.Edit, "pose": traits.ReferencePose,
	"first_frame": traits.FirstFrame, "last_frame": traits.LastFrame,
}

// InputsOfRoles is the inputs a model must take to use input images of the roles (each input once, in the order of the roles). A role that is not one of
// the input images' (a mask) gives none.
func InputsOfRoles(roles []string) []traits.Input {
	var out []traits.Input
	for _, r := range roles {
		if in, ok := roleInputs[r]; ok && !slices.Contains(out, in) {
			out = append(out, in)
		}
	}
	return out
}

// PreferForInputs puts the models that take the attached input images first (#1009): the more of the inputs a model takes, the earlier it comes. It is a
// stable sort, so models that take as many keep the order they had, and it keeps every model (a model that takes none is last, not hidden: a picker still
// lists it, and a run with it warns or stops, see the generation test's preflight). Without inputs the order does not change. list is returned
// sorted in place.
func PreferForInputs[T any](list []T, item func(T) Item, attached []traits.Input) []T {
	if len(attached) == 0 {
		return list
	}
	taken := func(e T) int {
		n := 0
		for _, in := range attached {
			if slices.Contains(item(e).Inputs, in) {
				n++
			}
		}
		return n
	}
	score := make([]int, len(list))
	idx := make([]int, len(list))
	for i, e := range list {
		score[i], idx[i] = taken(e), i
	}
	slices.SortStableFunc(idx, func(a, b int) int { return score[b] - score[a] })
	sorted := make([]T, len(list))
	for i, j := range idx {
		sorted[i] = list[j]
	}
	copy(list, sorted)
	return list
}
