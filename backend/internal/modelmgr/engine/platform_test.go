package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/engine"
)

func major(n int) func(context.Context) (int, error) {
	return func(context.Context) (int, error) { return n, nil }
}

// The bundled engine is for Windows and for a Mac with Apple silicon and macOS 14 or later; every other combination uses an external ComfyUI.
func TestTheBundledEngineIsSupportedOnWindowsAndOnAppleSiliconMacsOnly(t *testing.T) {
	e := catalog.Engine{Apple: &catalog.AppleEngine{MinMacOS: 14}}
	unreadable := func(context.Context) (int, error) { return 0, errors.New("sw_vers failed") }
	for name, tc := range map[string]struct {
		goos, goarch, root string
		macOS              func(context.Context) (int, error)
		want               bool
	}{
		"Windows":                {"windows", "amd64", "r", nil, true},
		"Windows on arm":         {"windows", "arm64", "r", nil, true},
		"an Apple silicon Mac":   {"darwin", "arm64", "r", major(14), true},
		"a newer macOS":          {"darwin", "arm64", "r", major(26), true},
		"macOS 13":               {"darwin", "arm64", "r", major(13), false},
		"macOS 12":               {"darwin", "arm64", "r", major(12), false},
		"an Intel Mac":           {"darwin", "amd64", "r", major(14), false},
		"an unreadable version":  {"darwin", "arm64", "r", unreadable, true},
		"a Mac and no directory": {"darwin", "arm64", "", major(14), false},
		"Linux":                  {"linux", "amd64", "r", nil, false},
		"Linux on arm":           {"linux", "arm64", "r", major(14), false},
		"another OS on arm":      {"freebsd", "arm64", "r", major(14), false},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, engine.Supported(e, tc.goos, tc.goarch, tc.root, tc.macOS))
		})
	}
	assert.False(t, engine.Supported(catalog.Engine{}, "darwin", "arm64", "r", major(15)), "a catalog without the macOS build has none for a Mac")
	assert.True(t, engine.Supported(catalog.Engine{}, "windows", "amd64", "r", nil))
}

func TestAMacOffersTheOneAppleBuildAndWindowsItsPortablePackages(t *testing.T) {
	e := catalog.Engine{
		Variants: map[catalog.Variant]catalog.Asset{catalog.VariantNVIDIA: {Label: "nvidia", Size: 1}},
		Apple:    &catalog.AppleEngine{Label: "apple", Size: 2, ExtractedSize: 3},
	}
	assert.Equal(t, []catalog.Variant{catalog.VariantApple}, engine.VariantList(e, "darwin"))
	assert.Equal(t, e.VariantList(), engine.VariantList(e, "windows"))
	assert.Equal(t, e.VariantList(), engine.VariantList(e, "linux"))

	a, ok := engine.AssetOf(e, catalog.VariantApple)
	assert.True(t, ok)
	assert.Equal(t, catalog.Asset{Label: "apple", Size: 2, ExtractedSize: 3}, a)
	a, ok = engine.AssetOf(e, catalog.VariantNVIDIA)
	assert.True(t, ok)
	assert.Equal(t, "nvidia", a.Label)
	_, ok = engine.AssetOf(e, catalog.VariantAMD)
	assert.False(t, ok, "a build the catalog has not pinned")
	_, ok = engine.AssetOf(catalog.Engine{}, catalog.VariantApple)
	assert.False(t, ok)

	assert.True(t, engine.Installable(e, "darwin", catalog.VariantApple))
	assert.False(t, engine.Installable(e, "darwin", catalog.VariantNVIDIA), "a Mac does not install a Windows package")
	assert.True(t, engine.Installable(e, "windows", catalog.VariantNVIDIA))
	assert.False(t, engine.Installable(e, "windows", catalog.VariantApple), "Windows does not install the Mac build")
	assert.False(t, engine.Installable(e, "windows", catalog.VariantAMD), "a build the catalog has not pinned")
}
