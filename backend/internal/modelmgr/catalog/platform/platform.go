// Package platform is the "GPU generation and OS" columns of the model catalog (Issue #979): which PCs a model is made for
// (Requires) and which it is known not to run on (Unsupported), and the warning the model manager gives a user whose PC is outside.
//
// It is a package of its own because the catalog directory is full (linterly), and it imports nothing of the catalog so that the catalog
// can hold its types. A warning never refuses an install: the columns say what the vendor documents, the PC may still be a case nobody
// tried, and an external ComfyUI may run on another machine than the one mam detects the GPU of.
package platform

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// The GPU generations a Host has (the values of Requires.GPUArch). The NVIDIA ones follow the compute capability (gpu.ArchOf).
const (
	ArchBlackwell = "blackwell" // compute capability 10.x and 12.x: B200, RTX 50, RTX PRO Blackwell
	ArchHopper    = "hopper"    // 9.x: H100, H200
	ArchAda       = "ada"       // 8.9: RTX 40, L4, L40
	ArchAmpere    = "ampere"    // 8.0, 8.6, 8.7: A100, RTX 30
	ArchTuring    = "turing"    // 7.5: RTX 20, GTX 16
	ArchVolta     = "volta"     // 7.0
	ArchPascal    = "pascal"    // 6.x: GTX 10
	ArchMaxwell   = "maxwell"   // 5.x
	ArchKepler    = "kepler"    // 3.x
	ArchAMD       = "amd"       // a Radeon (no finer generation is told)
	ArchIntel     = "intel"     // an Arc
	ArchApple     = "apple"     // the GPU of a Mac (macOS)
)

// The operating systems a Host has (runtime.GOOS).
const (
	OSWindows = "windows"
	OSDarwin  = "darwin"
	OSLinux   = "linux"
)

var archLabels = map[string]string{
	ArchBlackwell: "NVIDIA Blackwell（RTX 50 など）", ArchHopper: "NVIDIA Hopper", ArchAda: "NVIDIA Ada Lovelace（RTX 40 など）",
	ArchAmpere: "NVIDIA Ampere（RTX 30 など）", ArchTuring: "NVIDIA Turing（RTX 20 など）", ArchVolta: "NVIDIA Volta", ArchPascal: "NVIDIA Pascal（GTX 10 など）",
	ArchMaxwell: "NVIDIA Maxwell", ArchKepler: "NVIDIA Kepler", ArchAMD: "AMD Radeon", ArchIntel: "Intel Arc", ArchApple: "Mac の GPU（Apple）",
}

var osLabels = map[string]string{OSWindows: "Windows", OSDarwin: "macOS", OSLinux: "Linux"}

// Requires is where a model is meant to run: the PC must have one of GPUArch (when given) and run one of OS (when given). Reason says why, Source
// where that is written (the model card, ComfyUI's source, with the date it was read): only what a primary source states is filled in.
type Requires struct {
	GPUArch []string `json:"gpu_arch,omitempty"`
	OS      []string `json:"os,omitempty"`
	Reason  string   `json:"reason"`
	Source  string   `json:"source"`
}

// Unsupported is where a model is known not to run: on one of OS. Reason and Source are as in Requires.
type Unsupported struct {
	OS     []string `json:"os"`
	Reason string   `json:"reason"`
	Source string   `json:"source"`
}

// Columns are the two optional columns a catalog model (and its view for the screens) carries.
type Columns struct {
	Requires    *Requires    `json:"requires,omitempty"`
	Unsupported *Unsupported `json:"unsupported,omitempty"`
}

// Warning is the warning of the columns for h: see Warning.
func (c Columns) Warning(h Host) string { return Warning(c.Requires, c.Unsupported, h) }

// Validate checks the columns of one model; see Validate.
func (c Columns) Validate(where string) []error { return Validate(where, c.Requires, c.Unsupported) }

// Host is the PC the warning is about. A field that is "" is not known, and what is not known is never warned about.
type Host struct {
	// OS is runtime.GOOS of the machine mam runs on.
	OS string
	// GPUArch is the generation of the GPU with the most video memory (one of the Arch constants; "" when there is no GPU, or the generation of an
	// NVIDIA GPU cannot be told).
	GPUArch string
}

// Warning is what the screen says about a model for a Host ("" when the host is inside, or nothing is known that says it is not).
func Warning(r *Requires, u *Unsupported, h Host) string {
	var out []string
	if u != nil && h.OS != "" && slices.Contains(u.OS, h.OS) {
		out = append(out, fmt.Sprintf("この PC の OS（%s）では、このモデルは動かない可能性が高いです。%s", label(osLabels, h.OS), u.Reason))
	}
	if r != nil {
		if len(r.GPUArch) > 0 && h.GPUArch != "" && !slices.Contains(r.GPUArch, h.GPUArch) {
			out = append(out, fmt.Sprintf("この PC の GPU（%s）は、このモデルの対象外です（対象: %s）。%s", label(archLabels, h.GPUArch), labels(archLabels, r.GPUArch), r.Reason))
		}
		if len(r.OS) > 0 && h.OS != "" && !slices.Contains(r.OS, h.OS) {
			out = append(out, fmt.Sprintf("この PC の OS（%s）は、このモデルの対象外です（対象: %s）。%s", label(osLabels, h.OS), labels(osLabels, r.OS), r.Reason))
		}
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, " ") + " 導入はできますが、動かないことを承知のうえで進めてください。"
}

func label(m map[string]string, k string) string {
	if v, ok := m[k]; ok {
		return v
	}
	return k
}

func labels(m map[string]string, ks []string) string {
	out := make([]string, len(ks))
	for i, k := range ks {
		out[i] = label(m, k)
	}
	return strings.Join(out, "・")
}

// Validate checks the columns of one model: only known values, each column with its reason and a source that says when it was read, and no
// column that says nothing. where names the model in the messages.
func Validate(where string, r *Requires, u *Unsupported) []error {
	var errs []error
	if r != nil {
		if len(r.GPUArch) == 0 && len(r.OS) == 0 {
			errs = append(errs, fmt.Errorf("%s: requires names neither gpu_arch nor os", where))
		}
		errs = append(errs, validateValues(where+": requires.gpu_arch", r.GPUArch, archLabels)...)
		errs = append(errs, validateValues(where+": requires.os", r.OS, osLabels)...)
		errs = append(errs, validateWhy(where+": requires", r.Reason, r.Source)...)
	}
	if u != nil {
		if len(u.OS) == 0 {
			errs = append(errs, fmt.Errorf("%s: unsupported names no os", where))
		}
		errs = append(errs, validateValues(where+": unsupported.os", u.OS, osLabels)...)
		errs = append(errs, validateWhy(where+": unsupported", u.Reason, u.Source)...)
	}
	return errs
}

func validateValues(where string, vs []string, known map[string]string) []error {
	var errs []error
	for i, v := range vs {
		if _, ok := known[v]; !ok {
			errs = append(errs, fmt.Errorf("%s: %q is not a known value", where, v))
		}
		if slices.Contains(vs[:i], v) {
			errs = append(errs, fmt.Errorf("%s: %q is listed twice", where, v))
		}
	}
	return errs
}

// validateWhy wants the reason, and a source with the date it was read (20YY-MM-DD): a claim that nobody can check is not put in the catalog.
func validateWhy(where, reason, source string) []error {
	var errs []error
	if strings.TrimSpace(reason) == "" {
		errs = append(errs, fmt.Errorf("%s: the reason is required", where))
	}
	if !hasDate(source) {
		errs = append(errs, fmt.Errorf("%s: source must say where it is written and the date it was read (YYYY-MM-DD)", where))
	}
	return errs
}

var datePattern = regexp.MustCompile(`20[0-9]{2}-[0-9]{2}-[0-9]{2}`)

// hasDate reports whether text names a real calendar date (YYYY-MM-DD).
func hasDate(text string) bool {
	for _, d := range datePattern.FindAllString(text, -1) {
		if _, err := time.Parse(time.DateOnly, d); err == nil {
			return true
		}
	}
	return false
}
