// The suggestive series (sexy, duo) of the pose library: their own asset set, apart from the general one (Issue #1020).
package assetstest_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

// The zips of the sexy and duo series are the files of the v0.4.1 release of Shisei (the release's own digest, the size and the SHA-256 of the
// download that was checked on 2026-11-04), under the license of the images (CC0 1.0: commercial use allowed), and the set holds only what the
// age gate keeps back: the sexy_ and duo_ pictures (line art, depth maps, normal maps). It holds none of the general set's files, and the general
// set holds none of its (so that nothing of the series is on the disk until the age is confirmed and the user installs it).
func TestAdultPoseSetIsPinnedToTheReleaseAndHoldsOnlyTheSuggestiveSeries(t *testing.T) {
	c := catalog.Default()
	adult := setOf(t, c, "shisei-poses-adult")
	general := setOf(t, c, "shisei-poses")

	type pin struct {
		file, sha string
		size      int64
		members   int
		dir       string // where the entries lie in the zip
	}
	want := map[string]pin{
		"sexy":  {"shisei-sexy-v0.4.1.zip", "1df01ee7eba55bc3030d7a7bd6835870ca221c7bbd0540875146326bc2adf407", 9304519, 352, "line/"},
		"duo":   {"shisei-duo-v0.4.1.zip", "ca80b68bd479832a3bfba37e26790db6740950c4c87757ba4e52bb1eb3e808da", 7744472, 201, "line/"},
		"深度マップ": {"shisei-depth-v0.4.1.zip", "d572572ade47e73d5450da94836abea64c7a0ea9f266e8d162e512cd40e0a23d", 7110041, 427, "depth/"},
		"法線マップ": {"shisei-normal-v0.4.1.zip", "7271104b0e5c106eda147ee2204e2f08484cba44826ebb2b9c1211256bc2ff1e", 16185565, 427, "normal/"},
	}
	require.Len(t, adult.Assets, len(want))
	pictures := regexp.MustCompile(`^(sexy_S|duo_D)[0-9]{3}_(front|diag|side|above|below)\.webp$`)
	generalDests := map[string]bool{}
	for _, a := range general.Assets {
		for _, f := range a.Files() {
			generalDests[f.Dest] = true
		}
	}
	for key, w := range want {
		a := assetNamed(t, *adult, key)
		assert.Equal(t, "https://github.com/wquguru/shisei/releases/download/v0.4.1/"+w.file, a.URL, key)
		assert.Equal(t, w.sha, a.SHA256, key)
		assert.Equal(t, w.size, a.Size, key)
		assert.True(t, a.Required, key)
		assert.Equal(t, catalog.FormatZip, a.Format, key)
		l, ok := adult.License(a.License)
		require.True(t, ok, key)
		assert.Equal(t, modellicense.Yes, l.Commercial, key)
		assert.Contains(t, l.Name, "CC0", key)

		require.Len(t, a.Members, w.members, key)
		for _, m := range a.Members {
			name, ok := strings.CutPrefix(m.Entry, w.dir)
			require.True(t, ok, "%s: an entry of %s", m.Entry, w.dir)
			assert.Regexp(t, pictures, name, "only the pictures of the suggestive series")
			assert.Equal(t, strings.Replace(m.Entry, "line/", "poses/", 1), m.Dest, "the line art goes below poses/, the maps below depth/ and normal/, as the general set's")
			assert.Regexp(t, `^[0-9a-f]{64}$`, m.SHA256, m.Entry)
			assert.Positive(t, m.Size, m.Entry)
			assert.False(t, generalDests[m.Dest], "%s is the general set's file too", m.Dest)
		}
	}
	for _, a := range adult.Assets {
		assert.NotContains(t, a.Name+a.Dest, "catalog", "the names of the poses are the general set's catalog.json: the series' set holds pictures only")
		assert.Empty(t, a.Dest, "every asset of the set is a zip")
	}
}

// Every depth and normal map of the suggestive series has a line picture of the same name in the set, as the general set's do.
func TestAdultPoseMapsHaveALinePicture(t *testing.T) {
	adult := setOf(t, catalog.Default(), "shisei-poses-adult")
	line := map[string]bool{}
	for _, a := range adult.Assets {
		for _, m := range a.Members {
			if strings.HasPrefix(m.Entry, "line/") {
				line[strings.TrimPrefix(m.Entry, "line/")] = true
			}
		}
	}
	require.Len(t, line, 553)
	for _, a := range adult.Assets {
		for _, m := range a.Members {
			if kind, ok := strings.CutPrefix(m.Entry, "depth/"); ok {
				assert.True(t, line[kind], m.Entry)
			}
			if kind, ok := strings.CutPrefix(m.Entry, "normal/"); ok {
				assert.True(t, line[kind], m.Entry)
			}
		}
	}
}
