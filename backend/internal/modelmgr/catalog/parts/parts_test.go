package parts_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/parts"
)

var sha = strings.Repeat("a", 64)

func wheel(name string) parts.Part {
	return parts.Part{
		Name: name, Kind: parts.KindWheel, SHA256: sha, Size: 10, ExtractedSize: 30,
		URL: "https://files.pythonhosted.org/packages/ab/cd/ef01/" + name + "-1.0-py3-none-any.whl",
	}
}

func TestCheckSumsAGoodSetOfParts(t *testing.T) {
	py := parts.Part{
		Name: "python", Kind: parts.KindZip, SHA256: sha, Size: 5, ExtractedSize: 9, Dest: "python",
		URL: "https://www.python.org/ftp/python/3.10.11/python-3.10.11-embed-amd64.zip",
	}
	src := parts.Part{
		Name: "src", Kind: parts.KindZip, SHA256: sha, Size: 7, ExtractedSize: 11, Strip: "MuseTalk-0123",
		URL: "https://github.com/TMElyralab/MuseTalk/archive/0123456789abcdef0123456789abcdef01234567.zip",
	}
	size, extracted, errs := parts.Check("v", []parts.Part{py, src, wheel("numpy")})
	require.Empty(t, errs)
	assert.Equal(t, int64(22), size)
	assert.Equal(t, int64(50), extracted)
}

func TestCheckRefusesWhatIsNotPinnedOrNotWhereAPartMayCome(t *testing.T) {
	for name, tc := range map[string]struct {
		edit func(p *parts.Part)
		want string
	}{
		"another host":          {func(p *parts.Part) { p.URL = "https://example.com/x.whl" }, "not one of the places"},
		"http":                  {func(p *parts.Part) { p.URL = strings.Replace(p.URL, "https", "http", 1) }, "not one of the places"},
		"a source distribution": {func(p *parts.Part) { p.URL = "https://files.pythonhosted.org/packages/ab/cd/x-1.0.tar.gz" }, "not one of the places"},
		"no digest":             {func(p *parts.Part) { p.SHA256 = "" }, "sha256"},
		"an upper-case digest":  {func(p *parts.Part) { p.SHA256 = strings.ToUpper(sha) }, "sha256"},
		"no size":               {func(p *parts.Part) { p.Size = 0 }, "size and extracted_size"},
		"no extracted size":     {func(p *parts.Part) { p.ExtractedSize = 0 }, "size and extracted_size"},
		"no name":               {func(p *parts.Part) { p.Name = "" }, "a name"},
		"a kind of its own":     {func(p *parts.Part) { p.Kind = "tar" }, "zip or wheel"},
		"a wheel with a dest":   {func(p *parts.Part) { p.Dest = "x" }, "no dest or strip"},
		"a wheel with a strip":  {func(p *parts.Part) { p.Strip = "x" }, "no dest or strip"},
	} {
		t.Run(name, func(t *testing.T) {
			p := wheel("numpy")
			tc.edit(&p)
			_, _, errs := parts.Check("v", []parts.Part{p})
			require.NotEmpty(t, errs)
			assert.Contains(t, errs[0].Error(), tc.want)
		})
	}
	t.Run("a name twice", func(t *testing.T) {
		_, _, errs := parts.Check("v", []parts.Part{wheel("a"), wheel("a")})
		require.NotEmpty(t, errs)
		assert.Contains(t, errs[0].Error(), "used once")
	})
	for _, dest := range []string{"../x", "/x", `a\b`, "C:/x"} {
		t.Run("a zip into "+dest, func(t *testing.T) {
			p := parts.Part{Name: "z", Kind: parts.KindZip, SHA256: sha, Size: 1, ExtractedSize: 1, Dest: dest, URL: "https://www.python.org/ftp/python/3.10.11/python-3.10.11-embed-amd64.zip"}
			_, _, errs := parts.Check("v", []parts.Part{p})
			require.NotEmpty(t, errs)
			assert.Contains(t, errs[0].Error(), "relative slash paths")
		})
	}
}

func TestCheckBuild(t *testing.T) {
	overlays := map[string][]byte{"server.py": []byte("x")}
	good := &parts.Build{SitePackages: "python/Lib/site-packages", Files: []parts.File{
		{Dest: "python/python310._pth", Lines: []string{"."}}, {Dest: "server.py", Overlay: "server.py"},
	}}
	assert.Empty(t, parts.CheckBuild("r", good, overlays))

	for name, tc := range map[string]struct {
		b    *parts.Build
		want string
	}{
		"site-packages outside":  {&parts.Build{SitePackages: "../site"}, "site_packages"},
		"no site-packages":       {&parts.Build{}, "site_packages"},
		"a file outside":         {&parts.Build{SitePackages: "s", Files: []parts.File{{Dest: "../x", Lines: []string{"a"}}}}, "relative slash path"},
		"a file with both":       {&parts.Build{SitePackages: "s", Files: []parts.File{{Dest: "x", Lines: []string{"a"}, Overlay: "server.py"}}}, "an overlay or lines"},
		"a file with neither":    {&parts.Build{SitePackages: "s", Files: []parts.File{{Dest: "x"}}}, "an overlay or lines"},
		"an overlay that is not": {&parts.Build{SitePackages: "s", Files: []parts.File{{Dest: "x", Overlay: "nope.py"}}}, "no overlay"},
	} {
		t.Run(name, func(t *testing.T) {
			errs := parts.CheckBuild("r", tc.b, overlays)
			require.NotEmpty(t, errs)
			assert.Contains(t, errs[0].Error(), tc.want)
		})
	}
}
