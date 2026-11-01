package catalog

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
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

func validateRuntimes(rts []Runtime) []error {
	var errs []error
	seen := map[string]bool{}
	for _, r := range rts {
		if seen[r.ID] {
			errs = append(errs, fmt.Errorf("runtime %s: duplicate id", r.ID))
		}
		seen[r.ID] = true
		errs = append(errs, validateRuntime(r)...)
	}
	return errs
}

func validateRuntime(r Runtime) []error {
	where := "runtime " + r.ID
	var errs []error
	switch {
	case !runtimeIDPattern.MatchString(r.ID) || r.Name == "" || strings.TrimSpace(r.Description) == "":
		errs = append(errs, fmt.Errorf("%s: id (slug), name and description are required", where))
	case r.Purpose != PurposeTTS && r.Purpose != PurposeLipSync:
		errs = append(errs, fmt.Errorf("%s: purpose must be tts or lip_sync", where))
	case r.Version == "" || !runtimeSourcePattern.MatchString(r.SourceURL):
		errs = append(errs, fmt.Errorf("%s: version and a source_url at a GitHub tag or commit are required", where))
	case !relativeSlashPath(r.ArchiveRoot):
		errs = append(errs, fmt.Errorf("%s: archive_root %q must be a relative slash path", where, r.ArchiveRoot))
	case r.VRAM.MinGB <= 0 || strings.TrimSpace(r.VRAM.Source) == "":
		errs = append(errs, fmt.Errorf("%s: vram needs min_gb and the source of the guideline", where))
	}
	errs = append(errs, validateLicense(where, r.License)...)
	errs = append(errs, validateLaunch(where, r.Launch)...)
	errs = append(errs, validateSupport(where, r.Support)...)
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
		switch {
		case !runtimeArchivePattern.MatchString(a.URL):
			errs = append(errs, fmt.Errorf("%s: url must be https://huggingface.co/<repo>/resolve/<40-hex commit>/<name>.7z", vw))
		case !sha256Pattern.MatchString(a.SHA256):
			errs = append(errs, fmt.Errorf("%s: sha256 must be 64 lower-case hex digits", vw))
		case a.Size <= 0 || a.ExtractedSize <= a.Size || a.Label == "":
			errs = append(errs, fmt.Errorf("%s: size, extracted_size (larger) and label are required", vw))
		case a.MinComputeCap < 0:
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
