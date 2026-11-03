// Package posestest is a pose library over a fake GitHub for the tests of the pose gallery: a zip of pictures and the pose catalog the
// asset set pins, served by a local server, installed with the real installer.
package posestest

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/poses"
)

var testTime = time.Unix(1_700_000_000, 0)

// Sum is the SHA-256 of b as hex, the way the asset set pins a file.
func Sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// Picture is the content of the line picture with the name. The pictures are a few bytes of text each: only the names and the hashes matter
// to the library.
func Picture(name string) []byte { return []byte("img:" + name) }

// MapPicture is the content of the depth or normal map ("depth", "normal") with the name.
func MapPicture(kind, name string) []byte { return []byte(kind + ":" + name) }

// Entry is a pose of the fake catalog. Angles are the angles that have a line picture; Depth and Normal the angles that also have a depth or
// a normal map (the real library has the maps for most of the angles, not all).
type Entry struct {
	ID, Series, Rating, Class, Name, NameEN, Desc string
	Angles, Depth, Normal                         []string
}

func catalogJSON(t testing.TB, list []Entry) []byte {
	t.Helper()
	var out []map[string]any
	for _, e := range list {
		views := map[string]string{}
		for _, a := range e.Angles {
			views[a] = e.Series + "_" + e.ID + "_" + a + ".webp"
		}
		out = append(out, map[string]any{
			"id": e.ID, "series": e.Series, "rating": e.Rating, "class_jp": e.Class, "class_en": "C", "name_jp": e.Name, "name_en": e.NameEN, "desc_en": e.Desc, "views": views,
		})
	}
	data, err := json.Marshal(out)
	require.NoError(t, err)
	return data
}

func zipOf(t testing.TB, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

// Rig is a library over a fake GitHub that serves a small zip of line pictures, one of depth maps, one of normal maps and the pose catalog.
type Rig struct {
	Lib      *poses.Library
	Root     string
	Requests *atomic.Int32
	Dir      string // the directory of the installed files
}

// Seed are the poses of the fake pose catalog: P003 is rated suggestive (its picture is in the zip all the same), P009 has no picture.
var Seed = []Entry{
	{ID: "P001", Series: "std", Rating: "general", Class: "座り", Name: "仰け反り座り", NameEN: "Sitting leaning back", Desc: "sitting on the floor", Angles: []string{"front", "diag"},
		Depth: []string{"front"}, Normal: []string{"front", "diag"}},
	{ID: "P002", Series: "std", Rating: "general", Class: "立ち", Name: "腕を組んで立つ", NameEN: "Standing with arms crossed", Desc: "standing straight", Angles: []string{"front", "side", "above"},
		Depth: []string{"front", "side"}},
	{ID: "P003", Series: "std", Rating: "suggestive", Class: "座り", Name: "ひみつ", NameEN: "Hidden", Desc: "never shown", Angles: []string{"front"}},
	{ID: "U001", Series: "upper", Rating: "general", Class: "かわいい系", Name: "両手ハート", NameEN: "Both hands heart", Desc: "hands forming a heart", Angles: []string{"front"}},
	{ID: "P009", Series: "std", Rating: "general", Class: "立ち", Name: "写真なし", NameEN: "No picture", Desc: "has no file", Angles: []string{"front"}},
}

// pictureFiles are the files of the zip of the kind ("line", "depth", "normal"): the entry name in the zip -> content. P003 and the like
// are in the zips all the same (the zips hold what Shisei holds), P009 is in none.
func pictureFiles() map[string]map[string][]byte {
	out := map[string]map[string][]byte{"line": {}, "depth": {}, "normal": {}}
	for _, e := range Seed {
		if e.ID == "P009" {
			continue // listed in the catalog, not in the zip
		}
		for _, a := range e.Angles {
			name := e.Series + "_" + e.ID + "_" + a
			out["line"]["line/"+name+".webp"] = Picture(name)
		}
		for kind, angles := range map[string][]string{"depth": e.Depth, "normal": e.Normal} {
			for _, a := range angles {
				name := e.Series + "_" + e.ID + "_" + a
				out[kind][kind+"/"+name+".webp"] = MapPicture(kind, name)
			}
		}
	}
	return out
}

// New is a Rig with the library not installed yet.
func New(t testing.TB) *Rig {
	t.Helper()
	files := pictureFiles()
	zips := map[string][]byte{}
	for kind, f := range files {
		zips[kind] = zipOf(t, f)
	}
	cat := catalogJSON(t, Seed)
	reqs := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs.Add(1)
		for kind, body := range zips {
			if r.URL.Path == "/"+kind+".zip" {
				http.ServeContent(w, r, kind+".zip", testTime, bytes.NewReader(body))
				return
			}
		}
		if r.URL.Path == "/catalog.json" {
			http.ServeContent(w, r, "catalog.json", testTime, bytes.NewReader(cat))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	source := catalog.ResourceSource{Name: "Shisei", URL: "https://github.com/wquguru/shisei"}
	set := catalog.ResourceSet{
		Runtime:  poses.SetID,
		Licenses: []catalog.ResourceLicense{{ID: "cc0", Name: "CC0 1.0", URL: "https://example.com/cc0", Summary: "s", Commercial: "yes"}},
	}
	for _, kind := range []string{"line", "depth", "normal"} {
		var members []catalog.ResourceMember
		for entry, data := range files[kind] {
			dir := "poses"
			if kind != "line" {
				dir = kind
			}
			members = append(members, catalog.ResourceMember{Entry: entry, Dest: dir + "/" + filepath.Base(entry), Size: int64(len(data)), SHA256: Sum(data)})
		}
		set.Assets = append(set.Assets, catalog.Resource{Name: kind + " pictures", Format: catalog.FormatZip, URL: srv.URL + "/" + kind + ".zip", SHA256: Sum(zips[kind]),
			Size: int64(len(zips[kind])), Required: true, License: "cc0", Members: members, Source: source})
	}
	set.Assets = append(set.Assets, catalog.Resource{Name: "catalog", Dest: "catalog.json", URL: srv.URL + "/catalog.json", SHA256: Sum(cat), Size: int64(len(cat)), Required: true, License: "cc0", Source: source})
	root := filepath.Join(t.TempDir(), "localai")
	lib, err := poses.New(poses.Deps{
		Root: root, Catalog: &catalog.Catalog{ResourceSets: []catalog.ResourceSet{set}},
		Fetch: fetch.Options{
			CheckURL: func(*url.URL) error { return nil },
			Client:   fetch.WithRedirectRules(&http.Client{}, func(*url.URL) error { return nil }, ""),
			Retry:    fetch.Retry{Attempts: 1},
		},
		FreeBytes: func(string) (uint64, error) { return 1 << 40, nil },
	})
	require.NoError(t, err)
	return &Rig{Lib: lib, Root: root, Requests: reqs, Dir: filepath.Join(root, poses.SetID, "env")}
}

// Install installs the library the way the user does.
func (r *Rig) Install(t testing.TB) {
	t.Helper()
	require.NoError(t, r.Lib.Install(context.Background(), true, nil))
}
