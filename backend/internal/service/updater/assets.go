package updater

import (
	"errors"
	"fmt"
	"strings"
)

// Platform is the operating system and CPU architecture whose installer a Checker looks for in a release.
type Platform struct {
	OS, Arch string
}

// The platforms a release carries an installer for (scripts/package-windows.sh, scripts/package-macos.sh).
var (
	// PlatformWindowsAMD64 installs through the per-user NSIS installer (mam-<X.Y.Z>-windows-amd64-setup.exe).
	PlatformWindowsAMD64 = Platform{OS: "windows", Arch: "amd64"}
	// PlatformDarwinARM64 installs from the disk image of the unsigned Apple Silicon build (mam-<X.Y.Z>-darwin-arm64.dmg).
	PlatformDarwinARM64 = Platform{OS: "darwin", Arch: "arm64"}
)

// ErrUnsupportedPlatform is why a Checker built for a Platform that has no installer (DefaultPlatform's zero
// value) refuses to check: it never looks at a release, so it never picks an installer of another platform.
var ErrUnsupportedPlatform = errors.New("no installer is released for this platform")

// Supported reports whether a release carries an installer for p: false for the zero Platform
// (DefaultPlatform of a GOOS/GOARCH without one), on which the automatic update stays off.
func (p Platform) Supported() bool {
	return p == PlatformWindowsAMD64 || p == PlatformDarwinARM64
}

// DefaultPlatform is the platform of the installer a process running on goos/goarch updates itself with:
// the dmg on macOS on Apple Silicon, the Windows installer on Windows on amd64, and the zero Platform for every
// other GOOS/GOARCH (no installer is released for it, so the automatic update is off there: Supported). The
// headless server never builds a Checker.
func DefaultPlatform(goos, goarch string) Platform {
	switch p := (Platform{OS: goos, Arch: goarch}); p {
	case PlatformWindowsAMD64, PlatformDarwinARM64:
		return p
	}
	return Platform{}
}

// InstallerName is the one installer name a release with tag_name tag may carry for p, the name the packaging
// scripts give it: mam-<tag without its leading "v">-windows-amd64-setup.exe, or
// mam-<tag without its leading "v">-darwin-arm64.dmg. tag must already be valid semver (Checker.check verifies
// that first).
func (p Platform) InstallerName(tag string) string {
	name := "mam-" + strings.TrimPrefix(tag, "v") + "-" + p.OS + "-" + p.Arch
	if p == PlatformDarwinARM64 {
		return name + ".dmg"
	}
	return name + "-setup.exe"
}

// releaseAssets are the release assets the update pipeline uses.
type releaseAssets struct {
	installer, checksums Asset
	// signature is checksums.txt.sig; its Name is empty when the release has none.
	signature Asset
}

// selectAssets finds the installer of the release tagged tag for platform and its checksums.txt among
// assets, plus the optional detached signature checksums.txt.sig. The installer must be named
// exactly platform.InstallerName(tag): an installer of another version or platform never matches, and neither
// does the portable exe (mam-<X.Y.Z>-windows-amd64.exe), which is not installed automatically. The same
// name is the key of the checksums.txt line the download is verified against (readChecksum).
func selectAssets(assets []Asset, tag string, platform Platform) (releaseAssets, error) {
	installerName := platform.InstallerName(tag)
	var found releaseAssets
	foundInstaller, foundChecksums := false, false
	for _, a := range assets {
		switch a.Name {
		case installerName:
			found.installer, foundInstaller = a, true
		case checksumsAssetName:
			found.checksums, foundChecksums = a, true
		case signatureAssetName:
			found.signature = a
		}
	}
	switch {
	case !foundInstaller:
		return releaseAssets{}, fmt.Errorf("no %s asset found", installerName)
	case !foundChecksums:
		return releaseAssets{}, fmt.Errorf("no %s asset found", checksumsAssetName)
	}
	return found, nil
}
