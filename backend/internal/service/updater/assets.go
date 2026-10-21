package updater

import (
	"fmt"
	"path"
)

// releaseAssets are the release assets the update pipeline uses.
type releaseAssets struct {
	installer, checksums Asset
	// signature is checksums.txt.sig; its Name is empty when the release has none.
	signature Asset
}

// selectAssets finds the NSIS installer (mam-<X.Y.Z>-windows-amd64-setup.exe) and its checksums.txt
// among assets, plus the optional detached signature checksums.txt.sig. The portable exe of the
// release never matches installerAssetPattern: it is not installed automatically. Two installers
// are ambiguous, so the release is refused instead of guessing which one to run.
func selectAssets(assets []Asset) (releaseAssets, error) {
	var found releaseAssets
	installers, foundChecksums := 0, false
	for _, a := range assets {
		if ok, _ := path.Match(installerAssetPattern, a.Name); ok {
			found.installer = a
			installers++
			continue
		}
		switch a.Name {
		case checksumsAssetName:
			found.checksums, foundChecksums = a, true
		case signatureAssetName:
			found.signature = a
		}
	}
	switch {
	case installers == 0:
		return releaseAssets{}, fmt.Errorf("no %s asset found", installerAssetPattern)
	case installers > 1:
		return releaseAssets{}, fmt.Errorf("%d assets match %s, expected exactly one", installers, installerAssetPattern)
	case !foundChecksums:
		return releaseAssets{}, fmt.Errorf("no %s asset found", checksumsAssetName)
	}
	return found, nil
}
