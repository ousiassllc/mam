package traits

import (
	"fmt"
	"slices"
)

// VideoMode is the kind of a video model (Issue #1014): what the model is made to be given and to make. It is an item of the catalog entry
// of its own ("traits.video_modes"), written by the catalog author, and not computed from the workflows: the same inputs can come from
// different kinds of model (a VACE model takes a reference image like an image-to-video one does, but is not one). What the workflows say is
// only a check on it (ValidateVideoModes). The names are the ones of the filter, the query string and the MCP tools.
type VideoMode string

// The video modes.
const (
	ModeT2V   VideoMode = "t2v"   // text to video
	ModeI2V   VideoMode = "i2v"   // a first frame (and the text) to video
	ModeFLF2V VideoMode = "flf2v" // a first frame and a last frame to the video between them
	ModeVACE  VideoMode = "vace"  // the VACE family: one model for making and for editing a video with controls and references
	ModeS2V   VideoMode = "s2v"   // sound (speech, music) to video: a sound file is an input of the workflow (Issue #1153)
	ModeR2V   VideoMode = "r2v"   // reference images (people, objects, a background) to video
)

// videoModes is the order the video modes are listed in.
var videoModes = []VideoMode{ModeT2V, ModeI2V, ModeFLF2V, ModeVACE, ModeS2V, ModeR2V}

// AllVideoModes lists the video modes in the order the screens show them.
func AllVideoModes() []VideoMode { return slices.Clone(videoModes) }

// Valid reports whether m is a video mode.
func (m VideoMode) Valid() bool { return slices.Contains(videoModes, m) }

// Label is the short word of the badge and the filter.
func (m VideoMode) Label() string {
	switch m {
	case ModeT2V:
		return "T2V（文字から動画）"
	case ModeI2V:
		return "I2V（画像から動画）"
	case ModeFLF2V:
		return "FLF2V（先頭・最後のフレーム）"
	case ModeVACE:
		return "VACE（制御・参照つき）"
	case ModeS2V:
		return "S2V（音声から動画）"
	case ModeR2V:
		return "R2V（参照画像から動画）"
	}
	return string(m)
}

// ValidateVideoModes checks the video modes of one model: a local video model (video: its catalog kind is video, not an upscale or an
// interpolation) has at least one, every one is valid and none is written twice; any other model has none. For a video model they also have to
// agree with the inputs its workflows give (d):
//   - t2v is what a text-only workflow is: the model has t2v if and only if a workflow takes the text alone;
//   - i2v needs a workflow with a first frame, and a first frame is either i2v or (with a last frame) flf2v;
//   - flf2v needs a workflow with a first and a last frame, and a last frame is flf2v;
//   - r2v is a workflow that takes reference images, and a workflow with reference images is r2v;
//   - s2v is a workflow that takes a sound file (localcfg.ParamAudio), and a workflow with one is s2v.
//
// vace is not tied to an input (a VACE model is one of its own kind), so a model may carry it with any workflows.
func ValidateVideoModes(where string, modes []VideoMode, video bool, d Derived) []error {
	if !video {
		if len(modes) > 0 {
			return []error{fmt.Errorf("%s: traits.video_modes belong to a video model", where)}
		}
		return nil
	}
	if len(modes) == 0 {
		return []error{fmt.Errorf("%s: traits.video_modes is required for a video model (t2v, i2v, flf2v, vace, s2v, r2v)", where)}
	}
	var errs []error
	for i, m := range modes {
		switch {
		case !m.Valid():
			errs = append(errs, fmt.Errorf("%s: traits.video_modes %q must be t2v, i2v, flf2v, vace, s2v or r2v", where, m))
		case slices.Contains(modes[:i], m):
			errs = append(errs, fmt.Errorf("%s: traits.video_modes lists %q twice", where, m))
		}
	}
	has := func(m VideoMode) bool { return slices.Contains(modes, m) }
	check := func(ok bool, format string, args ...any) {
		if !ok {
			errs = append(errs, fmt.Errorf("%s: traits.video_modes: "+format, append([]any{where}, args...)...))
		}
	}
	check(has(ModeT2V) == d.Has(T2V), "t2v must be listed if and only if a workflow takes the text alone (workflows give t2v: %t)", d.Has(T2V))
	check(!has(ModeI2V) || d.Has(FirstFrame), "i2v needs a workflow that takes a first frame")
	check(!d.Has(FirstFrame) || has(ModeI2V) || has(ModeFLF2V), "a workflow takes a first frame: i2v or flf2v must be listed")
	check(!has(ModeFLF2V) || (d.Has(FirstFrame) && d.Has(LastFrame)), "flf2v needs a workflow that takes a first and a last frame")
	check(!d.Has(LastFrame) || has(ModeFLF2V), "a workflow takes a last frame: flf2v must be listed")
	check(has(ModeR2V) == d.Has(ReferenceWhole), "r2v must be listed if and only if a workflow takes reference images (workflows give reference: %t)", d.Has(ReferenceWhole))
	check(has(ModeS2V) == d.AudioInput, "s2v must be listed if and only if a workflow takes a sound file (workflows give an audio input: %t)", d.AudioInput)
	return errs
}
