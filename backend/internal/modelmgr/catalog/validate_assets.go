package catalog

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// hfRepoURL matches the Hugging Face repository a pinned file URL lies in.
var hfRepoURL = regexp.MustCompile(`^(https://huggingface\.co/[A-Za-z0-9._-]+/[A-Za-z0-9._-]+)/resolve/`)

// inRepoOf reports whether the page at source is in the Hugging Face repository the pinned file url lies in: then the file is the
// original, and a source note is not needed.
func inRepoOf(url, source string) bool {
	m := hfRepoURL.FindStringSubmatch(url)
	return m != nil && (source == m[1] || strings.HasPrefix(source, m[1]+"/"))
}

func validateResourceSets(c *Catalog) []error {
	var errs []error
	seen := map[string]bool{}
	for _, s := range c.ResourceSets {
		where := "assets " + s.Runtime
		if seen[s.Runtime] {
			errs = append(errs, fmt.Errorf("%s: duplicate runtime", where))
		}
		seen[s.Runtime] = true
		errs = append(errs, validateResourceSet(where, s)...)
	}
	for _, r := range c.Runtimes {
		if !seen[r.ID] {
			errs = append(errs, fmt.Errorf("runtime %s: no weights and dependencies (data/assets/%s.json)", r.ID, r.ID))
		}
	}
	return errs
}

func validateResourceSet(where string, s ResourceSet) []error {
	var errs []error
	if !runtimeIDPattern.MatchString(s.Runtime) || len(s.Assets) == 0 || len(s.Licenses) == 0 {
		errs = append(errs, fmt.Errorf("%s: a runtime id (slug), licenses and assets are required", where))
	}
	licenses := map[string]bool{}
	for _, l := range s.Licenses {
		if licenses[l.ID] {
			errs = append(errs, fmt.Errorf("%s: license %q is listed twice", where, l.ID))
		}
		licenses[l.ID] = true
		errs = append(errs, validateResourceLicense(where, l)...)
	}
	names, dests := map[string]bool{}, map[string]string{}
	required := false
	for _, a := range s.Assets {
		aw := where + " asset " + a.Name
		if names[a.Name] {
			errs = append(errs, fmt.Errorf("%s: listed twice", aw))
		}
		names[a.Name] = true
		required = required || a.Required
		if !licenses[a.License] {
			errs = append(errs, fmt.Errorf("%s: license %q is not one of the licenses of the set", aw, a.License))
		}
		errs = append(errs, validateResource(aw, a)...)
		for _, f := range a.Files() {
			if other, dup := dests[f.Dest]; dup {
				errs = append(errs, fmt.Errorf("%s: dest %s is also the dest of %s", aw, f.Dest, other))
			}
			dests[f.Dest] = a.Name
		}
	}
	if len(s.Assets) > 0 && !required {
		errs = append(errs, fmt.Errorf("%s: at least one asset must be required", where))
	}
	return errs
}

func validateResourceLicense(where string, l ResourceLicense) []error {
	lw := where + " license " + l.ID
	var errs []error
	if l.ID == "" || l.Name == "" || strings.TrimSpace(l.Summary) == "" || !strings.HasPrefix(l.URL, "https://") {
		errs = append(errs, fmt.Errorf("%s: id, name, an https url and a summary are required", lw))
	}
	switch l.CommercialUse {
	case CommercialYes:
	case CommercialNo, CommercialUnclear:
		if strings.TrimSpace(l.Notice) == "" {
			errs = append(errs, fmt.Errorf("%s: commercial_use %s needs a notice that says what to watch for", lw, l.CommercialUse))
		}
	default:
		errs = append(errs, fmt.Errorf("%s: commercial_use must be yes, no or unclear", lw))
	}
	return errs
}

func validateResource(aw string, a Resource) []error {
	var errs []error
	switch {
	case a.Name == "" || strings.TrimSpace(a.Description) == "":
		errs = append(errs, fmt.Errorf("%s: name and description are required", aw))
	case !modelURLPattern.MatchString(a.URL):
		errs = append(errs, fmt.Errorf("%s: url must be https://huggingface.co/<repo>/resolve/<40-hex commit>/<path>", aw))
	case !sha256Pattern.MatchString(a.SHA256):
		errs = append(errs, fmt.Errorf("%s: sha256 must be 64 lower-case hex digits", aw))
	case a.Size <= 0:
		errs = append(errs, fmt.Errorf("%s: size is required", aw))
	}
	src := a.Source
	switch {
	case src.Name == "" || !strings.HasPrefix(src.URL, "https://"):
		errs = append(errs, fmt.Errorf("%s: source needs a name and an https url of the original", aw))
	case !inRepoOf(a.URL, src.URL) && !hasCheckDate(src.Note):
		errs = append(errs, fmt.Errorf("%s: the file is not downloaded from its source: source_note must say where the original is and the date (YYYY-MM-DD) its SHA-256 was checked", aw))
	}
	return append(errs, validateResourceFiles(aw, a)...)
}

func validateResourceFiles(aw string, a Resource) []error {
	var errs []error
	switch {
	case !a.IsArchive():
		if a.Format != "" || len(a.Members) > 0 {
			errs = append(errs, fmt.Errorf("%s: a file has no format and no members", aw))
		}
		if !relativeSlashPath(a.Dest) || strings.HasSuffix(a.Dest, "/") {
			errs = append(errs, fmt.Errorf("%s: dest %q must be a relative slash path of a file", aw, a.Dest))
		}
	case a.Format != FormatZip && a.Format != FormatTarGz:
		errs = append(errs, fmt.Errorf("%s: format must be zip or tar.gz for an archive (without a dest)", aw))
	case len(a.Members) == 0:
		errs = append(errs, fmt.Errorf("%s: an archive needs its members", aw))
	}
	entries := map[string]bool{}
	for _, m := range a.Members {
		switch {
		case m.Entry == "" || strings.HasPrefix(m.Entry, "/") || strings.Contains(m.Entry, `\`) || slices.Contains(strings.Split(m.Entry, "/"), ".."):
			errs = append(errs, fmt.Errorf("%s: member entry %q is not a plain archive path", aw, m.Entry))
		case !relativeSlashPath(m.Dest) || strings.HasSuffix(m.Dest, "/"):
			errs = append(errs, fmt.Errorf("%s: member dest %q must be a relative slash path of a file", aw, m.Dest))
		case m.Size <= 0 || !sha256Pattern.MatchString(m.SHA256):
			errs = append(errs, fmt.Errorf("%s: member %s needs a size and a 64-digit sha256", aw, m.Entry))
		case entries[m.Entry]:
			errs = append(errs, fmt.Errorf("%s: member %s is listed twice", aw, m.Entry))
		}
		entries[m.Entry] = true
	}
	return errs
}
