package catalog_test

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

func setOf(t *testing.T, c *catalog.Catalog, runtime string) *catalog.ResourceSet {
	t.Helper()
	for i := range c.ResourceSets {
		if c.ResourceSets[i].Runtime == runtime {
			return &c.ResourceSets[i]
		}
	}
	t.Fatalf("no asset set for %s", runtime)
	return nil
}

func assetNamed(t *testing.T, s catalog.ResourceSet, name string) catalog.Resource {
	t.Helper()
	for _, a := range s.Assets {
		if strings.Contains(a.Name, name) {
			return a
		}
	}
	t.Fatalf("no asset %q", name)
	return catalog.Resource{}
}

// Every asset of the catalog is pinned to a Hugging Face commit with a SHA-256 and a size, says where its original is and has a
// license of the set; the files of an archive are pinned too.
func TestEmbeddedAssetSetsArePinnedWithSourceAndLicense(t *testing.T) {
	c := catalog.Default()
	require.Len(t, c.ResourceSets, 2)
	for _, s := range c.ResourceSets {
		for _, a := range s.Assets {
			assert.Regexp(t, `^https://huggingface\.co/[^/]+/[^/]+/resolve/[0-9a-f]{40}/`, a.URL, a.Name)
			assert.Regexp(t, `^[0-9a-f]{64}$`, a.SHA256, a.Name)
			assert.Positive(t, a.Size, a.Name)
			assert.NotEmpty(t, a.Source.Name+a.Source.URL, a.Name)
			_, ok := s.License(a.License)
			assert.True(t, ok, "%s: license %s", a.Name, a.License)
			for _, f := range a.Files() {
				assert.Regexp(t, `^[0-9a-f]{64}$`, f.SHA256, f.Dest)
			}
		}
	}
}

func TestGPTSoVITSAssetsHoldTheWeightsAndEveryDependencyThePackageNeeds(t *testing.T) {
	c := catalog.Default()
	s := setOf(t, c, "gpt-sovits")
	_, ok := c.Runtime(s.Runtime)
	require.True(t, ok, "the asset set belongs to a runtime of the catalog")

	dests := map[string]catalog.Resource{}
	for _, a := range s.Assets {
		assert.True(t, a.Required, a.Name)
		for _, f := range a.Files() {
			dests[f.Dest] = a
		}
	}
	// The files that the package's configuration (custom, v2) loads: the weights.
	for _, d := range []string{
		"GPT_SoVITS/pretrained_models/gsv-v2final-pretrained/s1bert25hz-5kh-longer-epoch=12-step=369668.ckpt",
		"GPT_SoVITS/pretrained_models/gsv-v2final-pretrained/s2G2333k.pth",
		"GPT_SoVITS/pretrained_models/chinese-hubert-base/pytorch_model.bin",
		"GPT_SoVITS/pretrained_models/chinese-roberta-wwm-ext-large/pytorch_model.bin",
	} {
		assert.Contains(t, dests, d)
	}
	// The files that a library fetches by itself when they are missing: none is left to a hidden download.
	for _, d := range []string{
		"GPT_SoVITS/pretrained_models/fast_langdetect/lid.176.bin",                               // fast_langdetect: Facebook
		"runtime/Lib/site-packages/pyopenjtalk/open_jtalk_dic_utf_8-1.11/sys.dic",                // pyopenjtalk: GitHub
		"runtime/nltk_data/corpora/cmudict/cmudict",                                              // nltk
		"runtime/nltk_data/taggers/averaged_perceptron_tagger/averaged_perceptron_tagger.pickle", // nltk
		"GPT_SoVITS/text/G2PWModel/g2pW.onnx",                                                    // g2pW
	} {
		assert.Contains(t, dests, d)
	}
	for d := range dests {
		assert.Regexp(t, `^(GPT_SoVITS|runtime)/`, d, "inside the package")
	}

	// The official weights are the author's own repository at a commit; a third party's copy says where the original is.
	w := assetNamed(t, *s, "SoVITS モデル")
	assert.Equal(t, "https://huggingface.co/lj1995/GPT-SoVITS", w.Source.URL)
	assert.Empty(t, w.Source.Note)
	l, _ := s.License(w.License)
	assert.Equal(t, catalog.CommercialYes, l.CommercialUse)
	assert.Contains(t, assetNamed(t, *s, "lid.176").Source.Note, "dl.fbaipublicfiles.com")
	assert.Contains(t, assetNamed(t, *s, "Open JTalk").Source.Note, "releases/download/v1.11.1")

	// What is not clear is said (the validation asks for the notice); g2pW has no license of its model that mam could confirm.
	assert.Equal(t, []string{"g2pW（中国語の読みの推定）"}, s.Unclear())
	// The archive of nltk holds two assets and counts once.
	assert.Less(t, s.TotalSize(), func() int64 {
		var n int64
		for _, a := range s.Assets {
			n += a.Size
		}
		return n
	}())
}

// MuseTalk's weights are recorded before its runtime is (#921): the six sources of the decision and what MuseTalk fetches by itself.
func TestMuseTalkAssetsListTheWeightsAndTheirLicenses(t *testing.T) {
	c := catalog.Default()
	s := setOf(t, c, "musetalk")
	_, hasRuntime := c.Runtime("musetalk")
	assert.False(t, hasRuntime, "the runtime package is #921's")

	for name, repo := range map[string]string{
		"MuseTalk 1.5 の重み": "TMElyralab/MuseTalk", "sd-vae-ft-mse の重み": "stabilityai/sd-vae-ft-mse", "whisper-tiny の重み": "openai/whisper-tiny",
		"DWPose の重み": "yzd-v/DWPose", "SyncNet": "ByteDance/LatentSync", "face-parse-bisent の重み": "ManyOtherFunctions/face-parse-bisent",
	} {
		assert.Contains(t, assetNamed(t, *s, name).URL, "https://huggingface.co/"+repo+"/resolve/", name)
	}
	// The S3FD weights are fetched by MuseTalk's face detection when they are not next to it.
	assert.Equal(t, "musetalk/utils/face_detection/detection/sfd/s3fd.pth", assetNamed(t, *s, "S3FD").Dest)

	// face-parse-bisent lies on Google Drive: mam takes a Hugging Face copy whose SHA-256 was checked against the original.
	fp := assetNamed(t, *s, "79999_iter.pth")
	assert.Equal(t, "468e13ca13a9b43cc0881a9f99083a430e9c0a38abd935431d1c28ee94b26567", fp.SHA256)
	assert.Contains(t, fp.Source.Note, "drive.google.com/open?id=154JgKpzCPW82qINcVieuPH3fZ2e0P812")
	assert.Contains(t, fp.Source.Note, "2026-11-01")

	unclear := map[string]bool{}
	for _, n := range s.Unclear() {
		unclear[n] = true
	}
	for _, n := range []string{"LatentSync の SyncNet（latentsync_syncnet.pt）", "face-parse-bisent の重み（79999_iter.pth）", "S3FD の顔検出の重み（s3fd-619a316812.pth）"} {
		assert.True(t, unclear[n], n)
	}
	for _, id := range []string{"mit-musetalk", "mit-sd-vae-ft-mse", "apache-2.0-whisper-tiny", "apache-2.0-dwpose"} {
		l, ok := s.License(id)
		require.True(t, ok, id)
		assert.Equal(t, catalog.CommercialYes, l.CommercialUse, id)
	}
}

func TestLoadReadsTheAssetSetsAndSortsThem(t *testing.T) {
	fsys := fstest.MapFS{
		"data/engine.json":     {Data: []byte(`{"version":"v1.0.0"}`)},
		"data/models/x.json":   {Data: []byte(`{"id":"x"}`)},
		"data/assets/b.json":   {Data: []byte(`{"runtime":"b"}`)},
		"data/assets/a.json":   {Data: []byte(`{"runtime":"a","assets":[{"name":"n"}]}`)},
		"data/runtimes/z.json": {Data: []byte(`{"id":"z"}`)},
	}
	c, err := catalog.Load(fsys)
	require.NoError(t, err)
	require.Len(t, c.ResourceSets, 2)
	assert.Equal(t, "a", c.ResourceSets[0].Runtime)
	_, ok := c.ResourceSet("b")
	assert.True(t, ok)
	_, ok = c.ResourceSet("nothing")
	assert.False(t, ok)

	fsys["data/assets/c.json"] = &fstest.MapFile{Data: []byte(`{"runtime":"c","bogus":1}`)}
	_, err = catalog.Load(fsys)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bogus")
}

func firstAsset(c *catalog.Catalog) *catalog.Resource { return &c.ResourceSets[0].Assets[0] }

func archiveAsset(t *testing.T, c *catalog.Catalog) *catalog.Resource {
	t.Helper()
	s := setOf(t, c, "gpt-sovits")
	for i := range s.Assets {
		if s.Assets[i].IsArchive() {
			return &s.Assets[i]
		}
	}
	t.Fatal("no archive")
	return nil
}

func TestValidateRefusesAnAssetThatIsNotPinnedOrNotDescribed(t *testing.T) {
	for name, tc := range map[string]struct {
		edit func(c *catalog.Catalog)
		want string
	}{
		"branch instead of a commit": {func(c *catalog.Catalog) {
			a := firstAsset(c)
			a.URL = strings.Replace(a.URL, a.URL[strings.Index(a.URL, "resolve/")+8:][:40], "main", 1)
		}, "40-hex commit"},
		"plain http": {func(c *catalog.Catalog) { a := firstAsset(c); a.URL = strings.Replace(a.URL, "https", "http", 1) }, "40-hex commit"},
		"a host that is not allowed": {func(c *catalog.Catalog) {
			a := firstAsset(c)
			a.URL = strings.Replace(a.URL, "huggingface.co", "example.com", 1)
		}, "40-hex commit"},
		"a GitHub release": {func(c *catalog.Catalog) {
			firstAsset(c).URL = "https://github.com/r9y9/open_jtalk/releases/download/v1.11.1/open_jtalk_dic_utf_8-1.11.tar.gz"
		}, "40-hex commit"},
		"no sha256":            {func(c *catalog.Catalog) { firstAsset(c).SHA256 = "" }, "sha256"},
		"a short sha256":       {func(c *catalog.Catalog) { firstAsset(c).SHA256 = firstAsset(c).SHA256[:63] }, "sha256"},
		"no size":              {func(c *catalog.Catalog) { firstAsset(c).Size = 0 }, "size is required"},
		"no name":              {func(c *catalog.Catalog) { firstAsset(c).Name = "" }, "name and description"},
		"no source":            {func(c *catalog.Catalog) { firstAsset(c).Source = catalog.ResourceSource{} }, "source needs a name"},
		"a source without url": {func(c *catalog.Catalog) { firstAsset(c).Source.URL = "" }, "source needs a name"},
		"no license":           {func(c *catalog.Catalog) { firstAsset(c).License = "" }, "is not one of the licenses"},
		"a license that is not listed": {func(c *catalog.Catalog) {
			firstAsset(c).License = "gpl-3.0"
		}, "is not one of the licenses"},
		"a copy whose original is elsewhere has no note": {func(c *catalog.Catalog) {
			firstAsset(c).Source.URL = "https://github.com/RVC-Boss/GPT-SoVITS"
		}, "source_note"},
		"a note without the date of the check": {func(c *catalog.Catalog) {
			a := firstAsset(c)
			a.Source.URL, a.Source.Note = "https://github.com/RVC-Boss/GPT-SoVITS", "原本は GitHub にある"
		}, "source_note"},
		"a note with a date that does not exist": {func(c *catalog.Catalog) {
			a := firstAsset(c)
			a.Source.URL, a.Source.Note = "https://github.com/RVC-Boss/GPT-SoVITS", "2026-13-45 に確認した"
		}, "source_note"},
		"an unclear license without a notice": {func(c *catalog.Catalog) {
			s := setOf(t, c, "musetalk")
			for i := range s.Licenses {
				if s.Licenses[i].CommercialUse == catalog.CommercialUnclear {
					s.Licenses[i].Notice = " "
				}
			}
		}, "needs a notice"},
		"a license that forbids commercial use without a notice": {func(c *catalog.Catalog) {
			l := &c.ResourceSets[0].Licenses[0]
			l.CommercialUse, l.Notice = catalog.CommercialNo, ""
		}, "needs a notice"},
		"a commercial value that is not one of three": {func(c *catalog.Catalog) {
			c.ResourceSets[0].Licenses[0].CommercialUse = "maybe"
		}, "yes, no or unclear"},
		"a license without a summary": {func(c *catalog.Catalog) { c.ResourceSets[0].Licenses[0].Summary = "" }, "a summary are required"},
		"a license url that is not https": {func(c *catalog.Catalog) {
			c.ResourceSets[0].Licenses[0].URL = "http://example.com"
		}, "an https url"},
		"the same license twice": {func(c *catalog.Catalog) {
			s := &c.ResourceSets[0]
			s.Licenses = append(s.Licenses, s.Licenses[0])
		}, "listed twice"},
		"the same asset twice": {func(c *catalog.Catalog) {
			s := &c.ResourceSets[0]
			s.Assets = append(s.Assets, s.Assets[0])
		}, "listed twice"},
		"the same destination twice": {func(c *catalog.Catalog) {
			s := &c.ResourceSets[0]
			s.Assets[1].Dest = s.Assets[0].Dest
		}, "is also the dest"},
		"a destination outside":                  {func(c *catalog.Catalog) { firstAsset(c).Dest = "../evil" }, "relative slash path"},
		"an absolute destination":                {func(c *catalog.Catalog) { firstAsset(c).Dest = "/etc/passwd" }, "relative slash path"},
		"a file with a format":                   {func(c *catalog.Catalog) { firstAsset(c).Format = catalog.FormatZip }, "no format and no members"},
		"an archive of a format that is unknown": {func(c *catalog.Catalog) { archiveAsset(t, c).Format = "rar" }, "zip or tar.gz"},
		"an archive without members":             {func(c *catalog.Catalog) { archiveAsset(t, c).Members = nil }, "needs its members"},
		"a member that climbs out": {func(c *catalog.Catalog) {
			archiveAsset(t, c).Members[0].Entry = "../x"
		}, "not a plain archive path"},
		"a member that is put outside": {func(c *catalog.Catalog) {
			archiveAsset(t, c).Members[0].Dest = "../x"
		}, "member dest"},
		"a member without a hash": {func(c *catalog.Catalog) { archiveAsset(t, c).Members[0].SHA256 = "" }, "needs a size and a 64-digit sha256"},
		"a member without a size": {func(c *catalog.Catalog) { archiveAsset(t, c).Members[0].Size = 0 }, "needs a size and a 64-digit sha256"},
		"a member twice": {func(c *catalog.Catalog) {
			a := archiveAsset(t, c)
			a.Members = append(a.Members, a.Members[0])
		}, "listed twice"},
		"nothing is required": {func(c *catalog.Catalog) {
			for i := range c.ResourceSets[0].Assets {
				c.ResourceSets[0].Assets[i].Required = false
			}
		}, "at least one asset must be required"},
		"no assets": {func(c *catalog.Catalog) { c.ResourceSets[0].Assets = nil }, "licenses and assets are required"},
		"a runtime without an asset set": {func(c *catalog.Catalog) {
			c.ResourceSets = c.ResourceSets[1:]
		}, "no weights and dependencies"},
		"two sets for a runtime": {func(c *catalog.Catalog) { c.ResourceSets = append(c.ResourceSets, c.ResourceSets[0]) }, "duplicate runtime"},
	} {
		t.Run(name, func(t *testing.T) {
			errs := mutate(t, tc.edit)
			require.NotEmpty(t, errs)
			assert.Contains(t, fmt.Sprint(errs), tc.want)
		})
	}
}

func TestACopyInTheSameRepositoryNeedsNoNote(t *testing.T) {
	errs := mutate(t, func(c *catalog.Catalog) {
		a := firstAsset(c)
		a.Source.URL = "https://huggingface.co/lj1995/GPT-SoVITS/tree/main"
		a.Source.Note = ""
	})
	assert.Empty(t, errs)
}
