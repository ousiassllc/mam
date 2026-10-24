// Package version holds what a release build stamps into the binary and where the app looks for
// its updates (spec/architecture/desktop.md §Auto update).
package version

// Version is the version of this build: a semver with the leading "v" (for example "v0.1.2") in a
// release build, set by the linker
//
//	-ldflags "-X github.com/ousiassllc/mam/backend/internal/version.Version=v0.1.2"
//
// "dev" (any value that is not a semver) is a build from a working tree: the updater does not
// check for updates at all.
var Version = "dev"

// ReleasePublicKey is the ed25519 public key (base64 of the raw 32 bytes) that the detached signature
// of a release's checksums.txt must verify under. A release build embeds it with
//
//	-X github.com/ousiassllc/mam/backend/internal/version.ReleasePublicKey=<base64>
//
// Empty (a dev build) skips the signature check with a warning.
var ReleasePublicKey = ""

// The repository whose Releases the installed app checks for updates: this one. The repository is to be made
// public, and the release assets live in its own Releases. While it is private, GitHub answers 404 for the
// latest release and the updater reports "no release" (Status.NoRelease), which is not a failure.
const (
	GitHubOwner = "ousiassllc"
	GitHubRepo  = "mam"
)
