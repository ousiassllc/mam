package updater

import (
	"fmt"
	"strings"
)

// releaseAssets are the release assets the update pipeline uses.
type releaseAssets struct {
	installer, checksums Asset
	// signature is checksums.txt.sig; its Name is empty when the release has none.
	signature Asset
}

// installerAssetName is the one installer name a release with tag_name tag may carry:
// mam-<tag without its leading "v">-windows-amd64-setup.exe, the name scripts/package-windows.sh
// gives it. tag must already be valid semver (Checker.check verifies that first).
func installerAssetName(tag string) string {
	return "mam-" + strings.TrimPrefix(tag, "v") + "-windows-amd64-setup.exe"
}

// selectAssets finds the NSIS installer of the release tagged tag and its checksums.txt among
// assets, plus the optional detached signature checksums.txt.sig. The installer must be named
// exactly installerAssetName(tag): an installer of another version never matches, and neither does
// the portable exe (mam-<X.Y.Z>-windows-amd64.exe), which is not installed automatically. The same
// name is the key of the checksums.txt line the download is verified against (readChecksum).
func selectAssets(assets []Asset, tag string) (releaseAssets, error) {
	installerName := installerAssetName(tag)
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
