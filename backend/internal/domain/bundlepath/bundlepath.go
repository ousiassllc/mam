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
// "NUL.mp4" and "con.tar.gz" as the device.
var reservedWindowsDeviceName = regexp.MustCompile(`(?i)^(con|prn|aux|nul|(com|lpt)[1-9])(\..*)?$`)

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
func ValidID(id string) bool {
	if id == "" || len(id) > 64 || reservedID.MatchString(id) {
		return false
	}
	for _, r := range id {
		if ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-'; !ok {
			return false
		}
	}
	return true
}
