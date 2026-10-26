package hfplan

import (
	"path"
	"regexp"
	"strings"
)

// maxNameLen is the longest file name that is installed.
const maxNameLen = 120

var (
	// namePattern is the characters of an installed file name: the set the catalog's file names use, plus nothing else.
	namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
	badChars    = regexp.MustCompile(`[^A-Za-z0-9._+-]+`)
	// windowsReserved are the device names Windows refuses as a file name (with any extension).
	windowsReserved = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[0-9]|lpt[0-9])$`)
	slugBad         = regexp.MustCompile(`[^a-z0-9]+`)
)

// ValidName reports whether name can be the file name of an install: a .safetensors or .sft name of the allowed
// characters, not a Windows device name, at most 120 characters.
func ValidName(name string) bool {
	if len(name) > maxNameLen || !namePattern.MatchString(name) || FormatOf(name) != FormatSafetensors {
		return false
	}
	stem, _, _ := strings.Cut(name, ".")
	return !windowsReserved.MatchString(stem)
}

// SanitizeName is the file name an install gets from the last part of a repository path: characters outside A-Z a-z 0-9
// . _ + - become "_", a leading symbol is dropped, a too long name is shortened (keeping its extension) and a Windows device
// name gets a prefix. The extension is kept as it is (.safetensors or .sft).
func SanitizeName(repoPath string) string {
	base := path.Base(repoPath)
	ext := badChars.ReplaceAllString(path.Ext(base), "_")
	stem := strings.TrimLeft(badChars.ReplaceAllString(strings.TrimSuffix(base, path.Ext(base)), "_"), "._+-")
	if stem == "" {
		stem = "model"
	}
	if first, _, _ := strings.Cut(stem, "."); windowsReserved.MatchString(first) {
		stem = "m_" + stem
	}
	if len(stem)+len(ext) > maxNameLen {
		stem = stem[:maxNameLen-len(ext)]
	}
	return stem + ext
}

// SuggestName is another name for a file whose name is taken by a different file: the name with the start of the SHA-256
// before the extension ("ae.safetensors" becomes "ae-1a2b3c4d.safetensors").
func SuggestName(name, sha256 string) string {
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	if len(stem) > maxNameLen-len(ext)-9 {
		stem = stem[:maxNameLen-len(ext)-9]
	}
	return stem + "-" + sha256[:min(8, len(sha256))] + ext
}

// WorkflowNames are the names of the two models (text to image, reference to image) an all-in-one SDXL checkpoint gets on the
// bundled endpoint: "hf-<slug of the file name>-<start of the SHA-256>" and the same with "-img2img". The SHA-256 keeps
// two files of one name apart and keeps a name of the catalog from being taken.
func WorkflowNames(name, sha256 string) (txt2img, img2img string) {
	slug := slugBad.ReplaceAllString(strings.ToLower(strings.TrimSuffix(name, path.Ext(name))), "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 36 {
		slug = strings.Trim(slug[:36], "-")
	}
	if slug == "" {
		slug = "model"
	}
	base := "hf-" + slug + "-" + sha256[:min(6, len(sha256))]
	return base, base + "-img2img"
}

// VRAM fits.
const (
	FitFits    = "fits"
	FitTight   = "tight"
	FitOver    = "over"
	FitUnknown = "unknown"
)

// VRAMFactor is the rough multiple of the file size the weights take on the GPU (the file plus the working memory). It
// is a rule of thumb from the size alone, not a measurement.
const VRAMFactor = 1.2

// fitsMargin is the part of the VRAM an estimate may take to be called "fits".
const fitsMargin = 0.85

// Fit compares the estimated need of a file (size × VRAMFactor) with the video memory: "fits" up to 85 % of it, "tight" up
// to all of it, "over" beyond. vram 0 (no GPU found, or not Windows) is "unknown". need is the estimate in bytes.
func Fit(size int64, vram uint64) (fit string, need int64) {
	need = int64(float64(size) * VRAMFactor)
	switch {
	case vram == 0:
		return FitUnknown, need
	case float64(need) <= float64(vram)*fitsMargin:
		return FitFits, need
	case float64(need) <= float64(vram):
		return FitTight, need
	}
	return FitOver, need
}
