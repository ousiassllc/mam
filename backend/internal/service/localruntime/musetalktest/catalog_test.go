package musetalktest_test

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/parts"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/lipsync"
)

const (
	weightPath = "/w/org/repo/resolve/0123456789abcdef0123456789abcdef01234567/unet.pth"
	weightDest = "models/musetalkV15/unet.pth"
)

var weight = bytes.Repeat([]byte("u"), 3000)

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range files {
		f, err := w.Create(name)
		require.NoError(t, err)
		_, err = f.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}

// museTalkCatalog is the real MuseTalk runtime (its support rules, its texts, its token) over a package of three fake parts and one fake
// weight, which a fake host serves.
func museTalkCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	real, ok := catalog.Default().Runtime("musetalk")
	require.True(t, ok)
	files := map[string][]byte{weightPath: weight}
	h := newHost(t, files)
	var ps []parts.Part
	var size, ext int64
	add := func(name, kind, path string, body []byte, edit func(*parts.Part)) {
		files[path] = body
		zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		require.NoError(t, err)
		var n int64
		for _, f := range zr.File {
			n += int64(f.UncompressedSize64) //nolint:gosec // a test archive
		}
		p := parts.Part{Name: name, Kind: kind, URL: h.URL + path, SHA256: sum256(body), Size: int64(len(body)), ExtractedSize: n}
		edit(&p)
		ps = append(ps, p)
		size += p.Size
		ext += p.ExtractedSize
	}
	add("python", parts.KindZip, "/python.zip", zipOf(t, map[string]string{"python.exe": "MZ", "python310._pth": "python310.zip"}),
		func(p *parts.Part) { p.Dest = "python" })
	add("source", parts.KindZip, "/source.zip", zipOf(t, map[string]string{"MuseTalk-abc/scripts/inference.py": "x", "MuseTalk-abc/musetalk/__init__.py": ""}),
		func(p *parts.Part) { p.Strip = "MuseTalk-abc" })
	add("torch", parts.KindWheel, "/torch.whl", zipOf(t, map[string]string{"torch/__init__.py": "", "torch-2.0.1.dist-info/METADATA": "Name: torch"}),
		func(*parts.Part) {})

	rt := real
	rt.Version = "test"
	rt.Build = &parts.Build{SitePackages: "python/Lib/site-packages", Files: []parts.File{{Dest: "mam_musetalk_server.py", Overlay: "mam_musetalk_server.py"}}}
	rt.Variants = map[catalog.Variant]catalog.RuntimeVariant{catalog.VariantNVIDIA: {Asset: catalog.Asset{Label: "test", Size: size, ExtractedSize: ext}, Parts: ps}}
	rt.Launch.StartTimeoutSec = 20
	set := catalog.ResourceSet{
		Runtime:  "musetalk",
		Licenses: []catalog.ResourceLicense{{ID: "mit", Name: "MIT", URL: "https://example.com/mit", Summary: "商用可", CommercialUse: catalog.CommercialYes}},
		Assets: []catalog.Resource{{
			Name: "MuseTalk 1.5 の重み", Description: "unet", Dest: weightDest, URL: h.URL + weightPath, SHA256: sum256(weight), Size: int64(len(weight)), Required: true,
			Source: catalog.ResourceSource{Name: "TMElyralab/MuseTalk", URL: "https://huggingface.co/TMElyralab/MuseTalk"}, License: "mit",
		}},
	}
	return &catalog.Catalog{Runtimes: []catalog.Runtime{rt}, ResourceSets: []catalog.ResourceSet{set}, Overlays: map[string][]byte{"mam_musetalk_server.py": []byte("# fake\n")}}
}

// inputs makes the files a request names: the video called name, a speech file and where the result goes.
func inputs(t *testing.T, name string) lipsync.Request {
	t.Helper()
	dir := t.TempDir()
	for _, f := range []string{name, "speech.wav", "ffmpeg"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o600))
	}
	return lipsync.Request{
		VideoPath: filepath.Join(dir, name), AudioPath: filepath.Join(dir, "speech.wav"), OutputPath: filepath.Join(dir, "out.mp4"), FFmpegPath: filepath.Join(dir, "ffmpeg"),
	}
}
