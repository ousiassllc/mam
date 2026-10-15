// Package bundlepath holds the limits on the IDs and file paths a Project bundle may carry
// (spec/requirements/non-functional.md §8). It depends on nothing, so the layers that read a
// bundle (repository, bundlezip, service) share one verdict; it is a sub-package of domain
// to keep the domain package under the line limit.
package bundlepath

import (
	"path"
	"regexp"
	"strings"
)

// ReviewInboxDir is the directory below assets/ that holds the videos of imported Review Packages.
const ReviewInboxDir = "reviews"

// ValidFilePath reports whether p is a safe asset-relative path of the Project: slash
// separated, clean, below the Project's own directory, with no ".." and no absolute or drive part,
// and every element after the Project directory is a name Windows can create: no device name (CON,
// NUL.mp4, COM1, ...), none of * ? " < > | or a control character, and no trailing dot or space
// (Windows drops it, so "a." and "a" would be one file).
func ValidFilePath(projectID, p string) bool {
	if !ValidID(projectID) || p == "" || strings.ContainsAny(p, "\\:\x00") {
		return false
	}
	if path.Clean(p) != p || !strings.HasPrefix(p, projectID+"/") || len(p) == len(projectID)+1 ||
		strings.Contains(p, "/../") || strings.HasSuffix(p, "/..") {
		return false
	}
	for _, name := range strings.Split(p[len(projectID)+1:], "/") {
		if !validFileName(name) {
			return false
		}
	}
	return true
}

// reservedWindowsDeviceName matches a Windows device name with or without an extension: Windows opens
// "NUL.mp4" and "con.tar.gz" as the device. COM and LPT take the superscript digits ¹ ² ³ too (Microsoft's
// "Naming Files, Paths, and Namespaces"); COM0 and LPT0 are not reserved here.
var reservedWindowsDeviceName = regexp.MustCompile(`(?i)^(con|prn|aux|nul|(com|lpt)([1-9]|[¹²³]))(\..*)?$`)

func validFileName(name string) bool {
	if name == "" || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") || reservedWindowsDeviceName.MatchString(name) {
		return false
	}
	return !strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f || strings.ContainsRune(`*?"<>|`, r) })
}

var reservedID = regexp.MustCompile(`(?i)^(` + ReviewInboxDir + `|con|prn|aux|nul|(com|lpt)[1-9])$`)

// ValidID reports whether id is safe as the Project's directory name, a file-path part, a header value and a
// URL part: a UUID or [A-Za-z0-9_-]{1,64}, but not ReviewInboxDir or a Windows device name (CON, NUL, COM1, ...),
// in any letter case (NTFS and APFS ignore it: "Reviews" is assets/reviews/). It is the one limit on an ID that reaches a URL.
func ValidID(id string) bool { return !reservedID.MatchString(id) && ValidShape(id) }

// ValidShape reports whether id is [A-Za-z0-9_-]{1,64}, reserved names or not: the limit on an ID that is
// stored and shown but does not name a directory of its own.
func ValidShape(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-'; !ok {
			return false
		}
	}
	return true
}

// Conflict reports whether two of the paths cannot both be written to a case-insensitive file system
// (NTFS, APFS): two spellings that differ only in letter case ("a.mp4" and "A.mp4" would be one file),
// or a path that is a file and also a directory above another path ("a" and "a/b"). The same spelling
// twice is one file, not a conflict. Each path is expected to have passed ValidFilePath.
func Conflict(paths []string) bool {
	files := make(map[string]string, len(paths)) // lower-cased path -> its spelling
	dirs := map[string]bool{}
	for _, p := range paths {
		key := strings.ToLower(p)
		if spelling, ok := files[key]; ok {
			if spelling != p {
				return true
			}
			continue
		}
		files[key] = p
		for d := path.Dir(key); d != "."; d = path.Dir(d) {
			dirs[d] = true
		}
	}
	for key := range files {
		if dirs[key] {
			return true
		}
	}
	return false
}
