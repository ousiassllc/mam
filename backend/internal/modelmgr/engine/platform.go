package engine

import (
	"context"
	"runtime"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
)

// Supported reports whether the bundled engine exists on this PC: on Windows, and on a Mac with Apple silicon (arm64) whose macOS is one
// the pinned wheels of the macOS build run on (catalog.AppleEngine.MinMacOS) and that has a directory to install to (root; the layout of
// macOS is empty without a data directory). An Intel Mac, an older macOS and every other OS use an external ComfyUI. goarch "" is this
// build's architecture; macOSMajor nil reads the version with sw_vers, and a version that cannot be read does not refuse the engine.
func Supported(e catalog.Engine, goos, goarch, root string, macOSMajor func(context.Context) (int, error)) bool {
	switch goos {
	case "windows":
		return true
	case "darwin":
		if goarch == "" {
			goarch = runtime.GOARCH
		}
		if e.Apple == nil || goarch != "arm64" || root == "" {
			return false
		}
		if macOSMajor == nil {
			macOSMajor = func(ctx context.Context) (int, error) { return gpu.MacOSMajor(ctx, gpu.ExecRunner{}) }
		}
		major, err := macOSMajor(context.Background())
		return err != nil || major >= e.Apple.MinMacOS
	}
	return false
}

// VariantList lists the builds goos can install, in a fixed order: the one build of a Mac, or the portable packages of Windows.
func VariantList(e catalog.Engine, goos string) []catalog.Variant {
	if goos == "darwin" {
		return []catalog.Variant{catalog.VariantApple}
	}
	return e.VariantList()
}

// AssetOf is the pinned download of a build: the label and the sizes (of the one archive, or of all the parts of the macOS build).
func AssetOf(e catalog.Engine, v catalog.Variant) (catalog.Asset, bool) {
	if v == catalog.VariantApple && e.Apple != nil {
		return e.Apple.Asset(), true
	}
	a, ok := e.Variants[v]
	return a, ok
}

// Installable reports whether v is a build goos can install and the catalog has pinned.
func Installable(e catalog.Engine, goos string, v catalog.Variant) bool {
	_, ok := AssetOf(e, v)
	return ok && slices.Contains(VariantList(e, goos), v)
}
