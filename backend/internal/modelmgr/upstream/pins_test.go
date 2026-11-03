package upstream_test

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/upstream"
)

// Every pinned URL of the catalog is checked, once, and each is one that the real download lets through.
func TestPins_CoverTheWholeCatalogWithOnlyDownloadableHosts(t *testing.T) {
	c := catalog.Default()
	pins := upstream.Pins(c)

	urls := map[string]bool{}
	for _, p := range pins {
		u, err := url.Parse(p.URL)
		require.NoError(t, err, p.Label)
		check := fetch.AllowedURL
		if p.Hosts == upstream.PackageHosts {
			check = fetch.PackageURL
		}
		assert.NoError(t, check(u), "%s: %s", p.Label, p.URL)
		assert.Regexp(t, `^[0-9a-f]{64}$`, p.SHA256, p.Label)
		assert.Positive(t, p.Size, p.Label)
		urls[p.URL] = true
	}

	want := func(u string) { assert.True(t, urls[u], "not in the pins: %s", u) }
	for _, v := range c.Engine.Variants {
		want(v.URL)
	}
	for _, m := range c.Models {
		for _, f := range m.Files {
			want(f.URL)
		}
	}
	for _, r := range c.Runtimes {
		for _, v := range r.Variants {
			for _, p := range v.Parts {
				want(p.URL)
			}
			if !v.Assembled() {
				want(v.URL)
			}
		}
	}
	for _, s := range c.ResourceSets {
		for _, a := range s.Assets {
			want(a.URL)
		}
	}
	for _, p := range c.Engine.Apple.Parts {
		want(p.URL)
	}
}

func TestPins_AreSortedAndOnePerDownload(t *testing.T) {
	pins := upstream.Pins(catalog.Default())
	for i := 1; i < len(pins); i++ {
		assert.LessOrEqual(t, pins[i-1].URL, pins[i].URL)
		assert.NotEqual(t, pins[i-1], pins[i])
	}
}

func TestPins_GatedModelsAreMarked(t *testing.T) {
	gated := 0
	for _, p := range upstream.Pins(catalog.Default()) {
		if p.Gated {
			gated++
			assert.Contains(t, p.URL, "huggingface.co/", p.Label)
		}
	}
	assert.Positive(t, gated, "the LTX-2.5 files are behind a login")
}
