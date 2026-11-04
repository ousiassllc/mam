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

// Rig is a library over a fake GitHub that serves a small zip of line pictures, one of depth maps, one of normal maps and the pose catalog, and the same
// three zips for the suggestive series (the asset set shisei-poses-adult). The age gate is Open: closed at first, the way the app starts.
type Rig struct {
	Lib      *poses.Library
	Root     string
	Requests *atomic.Int32
	Dir      string // the directory of the installed files
	AdultDir string // the directory of the installed files of the suggestive series
	// Open is the age gate the library asks (Deps.AdultOpen): store true to open it.
	Open *atomic.Bool
	// OnGate, when set, runs after the library asked the gate (with the answer it got): a test learns that a call reached its check, or changes
	// the gate between two checks.
	OnGate atomic.Pointer[func()]
}

// Seed are the poses of the fake pose catalog: P003 is rated suggestive (its picture is in the zip all the same), P009 has no picture. S001 and D001 are
// of the suggestive series (sexy, duo; D001 is rated general all the same, as six of Shisei's duo poses are): their pictures are in the zips of the
// suggestive set, never in the general one's.
var Seed = []Entry{
	{ID: "P001", Series: "std", Rating: "general", Class: "座り", Name: "仰け反り座り", NameEN: "Sitting leaning back", Desc: "sitting on the floor", Angles: []string{"front", "diag"},
		Depth: []string{"front"}, Normal: []string{"front", "diag"}},
	{ID: "P002", Series: "std", Rating: "general", Class: "立ち", Name: "腕を組んで立つ", NameEN: "Standing with arms crossed", Desc: "standing straight", Angles: []string{"front", "side", "above"},
		Depth: []string{"front", "side"}},
	{ID: "P003", Series: "std", Rating: "suggestive", Class: "座り", Name: "ひみつ", NameEN: "Hidden", Desc: "never shown", Angles: []string{"front"}},
	{ID: "U001", Series: "upper", Rating: "general", Class: "かわいい系", Name: "両手ハート", NameEN: "Both hands heart", Desc: "hands forming a heart", Angles: []string{"front"}},
	{ID: "P009", Series: "std", Rating: "general", Class: "立ち", Name: "写真なし", NameEN: "No picture", Desc: "has no file", Angles: []string{"front"}},
	{ID: "S001", Series: "sexy", Rating: "suggestive", Class: "立ち", Name: "S字立ち", NameEN: "Standing S-curve", Desc: "standing in an S-curve", Angles: []string{"front", "diag"},
		Depth: []string{"front"}, Normal: []string{"front"}},
	{ID: "D001", Series: "duo", Rating: "general", Class: "立ち", Name: "向き合い", NameEN: "Facing each other", Desc: "two figures face to face", Angles: []string{"front"}, Depth: []string{"front"}},
}

// pictureFiles are the files of the zip of the kind ("line", "depth", "normal") of the general set, or of the suggestive one (adult): the entry name in the
// zip -> content. P003 and the like are in the zips all the same (the zips hold what Shisei holds), P009 is in none.
func pictureFiles(adult bool) map[string]map[string][]byte {
	out := map[string]map[string][]byte{"line": {}, "depth": {}, "normal": {}}
	for _, e := range Seed {
		if e.ID == "P009" || poses.IsAdultSeries(e.Series) != adult {
			continue // P009: listed in the catalog, not in the zip
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

// assetSet is the asset set of the rig: a zip per kind of picture, served at <prefix><kind>.zip, and (the general set) the pose catalog.
func assetSet(id, prefix, serverURL string, files map[string]map[string][]byte, zips map[string][]byte, cat []byte) catalog.ResourceSet {
	source := catalog.ResourceSource{Name: "Shisei", URL: "https://github.com/wquguru/shisei"}
	set := catalog.ResourceSet{
		Runtime:  id,
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
		set.Assets = append(set.Assets, catalog.Resource{Name: prefix + kind + " pictures", Format: catalog.FormatZip, URL: serverURL + "/" + prefix + kind + ".zip", SHA256: Sum(zips[prefix+kind]),
			Size: int64(len(zips[prefix+kind])), Required: true, License: "cc0", Members: members, Source: source})
	}
	if cat != nil {
		set.Assets = append(set.Assets, catalog.Resource{Name: "catalog", Dest: "catalog.json", URL: serverURL + "/catalog.json", SHA256: Sum(cat), Size: int64(len(cat)), Required: true, License: "cc0", Source: source})
	}
	return set
}

// New is a Rig with the library not installed yet and the age gate closed.
func New(t testing.TB) *Rig {
	t.Helper()
	general, adult := pictureFiles(false), pictureFiles(true)
	zips := map[string][]byte{}
	for kind, f := range general {
		zips[kind] = zipOf(t, f)
	}
	for kind, f := range adult {
		zips["adult-"+kind] = zipOf(t, f)
	}
	cat := catalogJSON(t, Seed)
	reqs := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs.Add(1)
		for name, body := range zips {
			if r.URL.Path == "/"+name+".zip" {
				http.ServeContent(w, r, name+".zip", testTime, bytes.NewReader(body))
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

	root := filepath.Join(t.TempDir(), "localai")
	open := &atomic.Bool{}
	rig := &Rig{Root: root, Requests: reqs, Dir: filepath.Join(root, poses.SetID, "env"), AdultDir: filepath.Join(root, poses.AdultSetID, "env"), Open: open}
	lib, err := poses.New(poses.Deps{
		Root: root,
		Catalog: &catalog.Catalog{ResourceSets: []catalog.ResourceSet{
			assetSet(poses.SetID, "", srv.URL, general, zips, cat), assetSet(poses.AdultSetID, "adult-", srv.URL, adult, zips, nil),
		}},
		Fetch: fetch.Options{
			CheckURL: func(*url.URL) error { return nil },
			Client:   fetch.WithRedirectRules(&http.Client{}, func(*url.URL) error { return nil }, ""),
			Retry:    fetch.Retry{Attempts: 1},
		},
		FreeBytes: func(string) (uint64, error) { return 1 << 40, nil },
		AdultOpen: func() bool {
			answer := open.Load()
			if f := rig.OnGate.Load(); f != nil {
				(*f)()
			}
			return answer
		},
	})
	require.NoError(t, err)
	rig.Lib = lib
	return rig
}

// Install installs the library the way the user does.
func (r *Rig) Install(t testing.TB) {
	t.Helper()
	require.NoError(t, r.Lib.Install(context.Background(), true, nil))
}

// InstallAdult installs the suggestive series the way the user does: the age gate is opened for it (and left open), the main library installed first.
func (r *Rig) InstallAdult(t testing.TB) {
	t.Helper()
	r.Open.Store(true)
	r.Install(t)
	require.NoError(t, r.Lib.InstallAdult(context.Background(), true, nil))
}
