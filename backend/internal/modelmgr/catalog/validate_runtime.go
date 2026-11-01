package catalog

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/parts"
)

var (
	// A runtime id is a directory name below localai/, so it is a short slug.
	runtimeIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)
	// A runtime archive is pinned to a commit of a Hugging Face repository (never a branch such as main), like a model file.
	runtimeArchivePattern = regexp.MustCompile(`^https://huggingface\.co/[A-Za-z0-9._-]+/[A-Za-z0-9._-]+/resolve/[0-9a-f]{40}/[A-Za-z0-9][A-Za-z0-9._+-]*\.7z$`)
	// The source of a runtime is a tag or a commit of a GitHub repository.
	runtimeSourcePattern = regexp.MustCompile(`^https://github\.com/[A-Za-z0-9._-]+/[A-Za-z0-9._-]+/(tree|commit)/[A-Za-z0-9._-]+$`)
	envPattern           = regexp.MustCompile(`^[A-Z][A-Z0-9_]*=.*$`)
)

// relativeSlashPath reports whether p is a non-empty relative path with "/" separators that stays below its directory.
func relativeSlashPath(p string) bool {
	return p != "" && !strings.HasPrefix(p, "/") && !strings.Contains(p, `\`) && !strings.Contains(p, ":") &&
		!slices.Contains(strings.Split(p, "/"), "..")
}

func validateRuntimes(c *Catalog) []error {
	var errs []error
	seen := map[string]bool{}
	for _, r := range c.Runtimes {
		if seen[r.ID] {
			errs = append(errs, fmt.Errorf("runtime %s: duplicate id", r.ID))
		}
		seen[r.ID] = true
		errs = append(errs, validateRuntime(c, r)...)
	}
	return errs
}

func validateRuntime(c *Catalog, r Runtime) []error {
	where := "runtime " + r.ID
	var errs []error
	switch {
	case !runtimeIDPattern.MatchString(r.ID) || r.Name == "" || strings.TrimSpace(r.Description) == "":
		errs = append(errs, fmt.Errorf("%s: id (slug), name and description are required", where))
	case r.Purpose != PurposeTTS && r.Purpose != PurposeLipSync:
		errs = append(errs, fmt.Errorf("%s: purpose must be tts or lip_sync", where))
	case r.Version == "" || !runtimeSourcePattern.MatchString(r.SourceURL):
		errs = append(errs, fmt.Errorf("%s: version and a source_url at a GitHub tag or commit are required", where))
	case r.Build == nil && !relativeSlashPath(r.ArchiveRoot):
		errs = append(errs, fmt.Errorf("%s: archive_root %q must be a relative slash path", where, r.ArchiveRoot))
	case r.Build != nil && r.ArchiveRoot != "":
		errs = append(errs, fmt.Errorf("%s: an assembled runtime has parts, not an archive_root", where))
	case r.VRAM.MinGB <= 0 || strings.TrimSpace(r.VRAM.Source) == "":
		errs = append(errs, fmt.Errorf("%s: vram needs min_gb and the source of the guideline", where))
	}
	errs = append(errs, validateLicense(where, r.License)...)
	errs = append(errs, validateLaunch(where, r.Launch)...)
	errs = append(errs, validateSupport(where, r.Support)...)
	if r.Build != nil {
		errs = append(errs, parts.CheckBuild(where, r.Build, c.Overlays)...)
	}
	return append(errs, validateRuntimeVariants(where, r)...)
}

func validateLaunch(where string, l RuntimeLaunch) []error {
	var errs []error
	if !relativeSlashPath(l.Entry) || !slices.Contains(l.Args, l.Entry) {
		errs = append(errs, fmt.Errorf("%s: launch entry must be a relative slash path that args run", where))
	}
	if !relativeSlashPath(l.Python) || !strings.HasPrefix(l.ReadyPath, "/") || strings.ContainsAny(l.ReadyPath, "?# ") {
		errs = append(errs, fmt.Errorf("%s: launch python must be a relative slash path and ready_path an absolute URL path", where))
	}
	if slices.ContainsFunc(l.PathDirs, func(d string) bool { return !relativeSlashPath(d) }) {
		errs = append(errs, fmt.Errorf("%s: launch path_dirs must be relative slash paths", where))
	}
	if !slices.Contains(l.Args, "{port}") || !slices.Contains(l.Args, "127.0.0.1") || slices.Contains(l.Args, "0.0.0.0") || slices.Contains(l.Args, "::") {
		errs = append(errs, fmt.Errorf("%s: launch args must listen on 127.0.0.1 and the {port}, never on every interface", where))
	}
	if l.StartTimeoutSec <= 0 || len(l.Env) == 0 {
		errs = append(errs, fmt.Errorf("%s: launch needs start_timeout_sec and the offline env", where))
	}
	for _, e := range l.Env {
		if !envPattern.MatchString(e) {
			errs = append(errs, fmt.Errorf("%s: launch env %q must be NAME=value", where, e))
		}
	}
	return errs
}

func validateSupport(where string, s RuntimeSupport) []error {
	var errs []error
	reasons := []string{s.ArchReason, s.NoGPUReason, s.NotNVIDIAReason, s.LowVRAMReason, s.Fallback, s.OSReasons["default"]}
	if s.OS == "" || s.Arch == "" || slices.ContainsFunc(reasons, func(t string) bool { return strings.TrimSpace(t) == "" }) {
		errs = append(errs, fmt.Errorf("%s: support needs os, arch, the reasons (os_reasons.default too) and the fallback", where))
	}
	if (s.MaxComputeCap != 0 || s.ComputeCapReason != "") && (s.MaxComputeCap <= 0 || strings.TrimSpace(s.ComputeCapReason) == "") {
		errs = append(errs, fmt.Errorf("%s: support max_compute_cap and compute_cap_reason go together", where))
	}
	if _, own := s.OSReasons[s.OS]; own {
		errs = append(errs, fmt.Errorf("%s: support os_reasons must not give a reason for the supported OS %s", where, s.OS))
	}
	for os, reason := range s.OSReasons {
		if strings.TrimSpace(reason) == "" {
			errs = append(errs, fmt.Errorf("%s: support os_reasons.%s is empty", where, os))
		}
	}
	return errs
}

func validateRuntimeVariants(where string, r Runtime) []error {
	var errs []error
	if len(r.Variants) == 0 {
		return []error{fmt.Errorf("%s: at least one variant is required", where)}
	}
	base := 0
	for _, v := range r.VariantList() {
		a := r.Variants[v]
		vw := fmt.Sprintf("%s variant %s", where, v)
		if a.Assembled() != (r.Build != nil) {
			errs = append(errs, fmt.Errorf("%s: parts and the runtime's build go together", vw))
		} else if a.Assembled() {
			errs = append(errs, validateAssembled(vw, a)...)
		} else {
			errs = append(errs, validateArchiveVariant(vw, a)...)
		}
		if a.MinComputeCap < 0 {
			errs = append(errs, fmt.Errorf("%s: min_compute_cap must not be negative", vw))
		}
		if a.MinComputeCap == 0 {
			base++
		}
	}
	if base != 1 {
		errs = append(errs, fmt.Errorf("%s: exactly one variant (min_compute_cap 0) is for the GPUs without a variant of their own", where))
	}
	return errs
}

func validateArchiveVariant(vw string, a RuntimeVariant) []error {
	switch {
	case !runtimeArchivePattern.MatchString(a.URL):
		return []error{fmt.Errorf("%s: url must be https://huggingface.co/<repo>/resolve/<40-hex commit>/<name>.7z", vw)}
	case !sha256Pattern.MatchString(a.SHA256):
		return []error{fmt.Errorf("%s: sha256 must be 64 lower-case hex digits", vw)}
	case a.Size <= 0 || a.ExtractedSize <= a.Size || a.Label == "":
		return []error{fmt.Errorf("%s: size, extracted_size (larger) and label are required", vw)}
	}
	return nil
}

func validateAssembled(vw string, a RuntimeVariant) []error {
	size, extracted, errs := parts.Check(vw, a.Parts)
	if a.Label == "" || a.URL != "" || a.SHA256 != "" {
		errs = append(errs, fmt.Errorf("%s: a build of parts has a label and no url or sha256 of its own", vw))
	}
	if a.Size != size || a.ExtractedSize != extracted {
		errs = append(errs, fmt.Errorf("%s: size %d and extracted_size %d must be the sums of the parts (%d, %d)", vw, a.Size, a.ExtractedSize, size, extracted))
	}
	return errs
}
