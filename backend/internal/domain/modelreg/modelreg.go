// Package modelreg holds the identifiers of the image and video models defined on registered endpoints
// (spec/architecture/local-models.md): the models the user adds and removes while the app runs. domain.ImageModels
// and domain.VideoModels append them to the built-in models, so every place that validates or lists a model
// name (the locks of a Shot, the Director's commands, the settings screen) sees them without a handle on the
// Model Adapter. The set is process-wide and replaced as a whole when the settings change. It is a leaf (strings
// only) so that package domain can import it.
package modelreg

import (
	"slices"
	"strings"
	"sync/atomic"
)

type set struct{ image, video []string }

var configured atomic.Pointer[set]

// Set replaces the identifiers of the configured models.
func Set(image, video []string) {
	configured.Store(&set{image: slices.Clone(image), video: slices.Clone(video)})
}

// Image is builtin followed by the configured image models (the identifiers of the models defined on
// registered endpoints); it is builtin itself when there are none.
func Image[T ~string](builtin ...T) []T {
	return with(builtin, func(s *set) []string { return s.image })
}

// Video is builtin followed by the configured video models.
func Video[T ~string](builtin ...T) []T {
	return with(builtin, func(s *set) []string { return s.video })
}

func with[T ~string](builtin []T, pick func(*set) []string) []T {
	s := configured.Load()
	if s == nil || len(pick(s)) == 0 {
		return builtin
	}
	out := slices.Clone(builtin)
	for _, id := range pick(s) {
		if !slices.Contains(out, T(id)) {
			out = append(out, T(id))
		}
	}
	return out
}

// Join renders models separated by sep ("kling-3.0 / seedance-2.0" for " / ").
func Join[T ~string](models []T, sep string) string {
	names := make([]string, len(models))
	for i, m := range models {
		names[i] = string(m)
	}
	return strings.Join(names, sep)
}
