package poses_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/poses"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/poses/posestest"
)

func sum(b []byte) string { return posestest.Sum(b) }

func ids(list []poses.Pose) []string {
	var out []string
	for _, p := range list {
		out = append(out, p.ID)
	}
	return out
}

// Nothing is fetched before the user agrees, and the library says it is not installed until every file is in place: the gallery must
// never show a pose whose picture is not there.
func TestNothingIsFetchedWithoutAgreement(t *testing.T) {
	r := posestest.New(t)
	assert.False(t, r.Lib.Status().Installed)
	_, err := r.Lib.Poses()
	assert.ErrorIs(t, err, poses.ErrNotInstalled)

	err = r.Lib.Install(context.Background(), false, nil)
	assert.ErrorIs(t, err, poses.ErrNotAccepted)
	assert.Zero(t, r.Requests.Load(), "a refused install fetches nothing")
	assert.False(t, r.Lib.Status().Installed)
}

func TestInstalledLibraryListsOnlyGeneralPosesThatHavePictures(t *testing.T) {
	r := posestest.New(t)
	var done, total int64
	require.NoError(t, r.Lib.Install(context.Background(), true, func(d, tt int64) { done, total = d, tt }))
	assert.Positive(t, total)
	assert.Equal(t, total, done, "the progress ends at the total")

	st := r.Lib.Status()
	assert.True(t, st.Installed)
	assert.Equal(t, 4, st.Poses, "the poses the asset set names pictures for (the zip of this test holds the suggestive P003 too)")

	all, err := r.Lib.Poses()
	require.NoError(t, err)
	assert.Equal(t, []string{"P001", "P002", "U001"}, ids(all), "the suggestive pose and the pose without a picture are not listed; the order is the catalog's")
	p1, ok := r.Lib.Pose("P001")
	require.True(t, ok)
	assert.Equal(t, "std_P001_front", p1.Image(poses.Front))
	assert.Equal(t, "", p1.Image(poses.Below), "an angle without a picture is empty")
	assert.Equal(t, poses.Front, p1.FirstAngle(poses.Below), "another angle (the first it has) stands in for the one that is missing")
	assert.Equal(t, poses.Diag, p1.FirstAngle(poses.Diag))
	_, ok = r.Lib.Pose("P003")
	assert.False(t, ok)
}

func TestFileIsOnlyAPictureOfAnInstalledPose(t *testing.T) {
	r := posestest.New(t)
	_, err := r.Lib.File("std_P001_front")
	assert.ErrorIs(t, err, poses.ErrNotInstalled)
	r.Install(t)

	path, err := r.Lib.File("std_P001_front")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(r.Dir, "poses", "std_P001_front.webp"), path)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, posestest.Picture("std_P001_front"), got)

	for _, bad := range []string{"../catalog", "std_P001_front/../../x", "std_P001_front.webp", "", "std_P003_front", "std_P009_front", "upper_U001_below"} {
		_, err := r.Lib.File(bad)
		var invalid *gterr.InvalidError
		assert.ErrorAs(t, err, &invalid, bad)
	}
}

func TestAFileThatWasReplacedOrRemovedTakesTheLibraryOutUntilItIsInstalledAgain(t *testing.T) {
	r := posestest.New(t)
	r.Install(t)
	target := filepath.Join(r.Dir, "poses", "std_P002_side.webp")

	require.NoError(t, os.Remove(target))
	_, err := r.Lib.Poses()
	assert.ErrorIs(t, err, poses.ErrNotInstalled, "a missing picture")
	assert.False(t, r.Lib.Status().Installed)
	assert.Positive(t, r.Lib.Status().NeedBytes)

	r.Install(t) // downloads again what is missing
	_, err = r.Lib.Poses()
	require.NoError(t, err)

	// Same size, other bytes: the install hashes the file, finds it is not the pinned one and downloads it again.
	require.NoError(t, os.WriteFile(target, bytes.Repeat([]byte("x"), len(posestest.Picture("std_P002_side"))), 0o600))
	_, err = r.Lib.Poses()
	assert.ErrorIs(t, err, poses.ErrNotInstalled, "a replaced picture is not trusted")
	r.Install(t)
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, posestest.Picture("std_P002_side"), got)
}

func libraryOver(t *testing.T, asset catalog.Resource) *poses.Library {
	t.Helper()
	lib, err := poses.New(poses.Deps{
		Root:    filepath.Join(t.TempDir(), "localai"),
		Catalog: &catalog.Catalog{ResourceSets: []catalog.ResourceSet{{Runtime: poses.SetID, Assets: []catalog.Resource{asset}}}},
		Fetch: fetch.Options{
			CheckURL: func(*url.URL) error { return nil }, Client: fetch.WithRedirectRules(&http.Client{}, func(*url.URL) error { return nil }, ""),
			Retry: fetch.Retry{Attempts: 1},
		},
		FreeBytes: func(string) (uint64, error) { return 1 << 40, nil },
	})
	require.NoError(t, err)
	return lib
}

// A download that cannot be trusted is thrown away and the user is told in words: the source answered "gone", or the bytes are not the ones
// the catalog pinned (the library must stay uninstalled either way).
func TestInstallSaysWhyADownloadWasRefused(t *testing.T) {
	body := []byte("[]")
	for name, tc := range map[string]struct {
		status  int
		sha     string
		message string
	}{
		"the source is gone":     {http.StatusGone, sum(body), "HTTP 410"},
		"not the pinned content": {http.StatusOK, sum([]byte("something else")), "SHA-256"},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.status != http.StatusOK {
					http.Error(w, "gone", tc.status)
					return
				}
				_, _ = w.Write(body)
			}))
			t.Cleanup(srv.Close)
			lib := libraryOver(t, catalog.Resource{Name: "catalog", Dest: "catalog.json", URL: srv.URL + "/catalog.json", SHA256: tc.sha, Size: int64(len(body)), Required: true})
			err := lib.Install(context.Background(), true, nil)
			var invalid *gterr.InvalidError
			require.ErrorAs(t, err, &invalid)
			assert.Contains(t, invalid.Message, tc.message)
			assert.False(t, lib.Status().Installed)
		})
	}
}

// The library installed from the embedded catalog is pinned: it names the pictures of general-rated poses only, from GitHub.
func TestTheEmbeddedPoseLibraryIsPinnedAndHoldsNoSuggestivePose(t *testing.T) {
	set, ok := catalog.Default().ResourceSet(poses.SetID)
	require.True(t, ok)
	lib, err := poses.New(poses.Deps{Root: filepath.Join(t.TempDir(), "localai")})
	require.NoError(t, err)
	st := lib.Status()
	assert.False(t, st.Installed)
	assert.Equal(t, set.TotalSize(), st.DownloadBytes)
	assert.Equal(t, 96, st.Poses, "the poses of Shisei v0.4.1 that are rated general and have a picture: 69 full-body and 27 upper-body")
	assert.Equal(t, 414, st.Images)
	assert.Equal(t, "https://github.com/wquguru/shisei", st.Source)
	assert.Less(t, st.DownloadBytes, int64(25<<20), "20 MB, not the 150 MB of the originals")
	for _, l := range st.Licenses {
		assert.NotEmpty(t, l.Summary)
	}
}

func TestFilterSearchesNamesCategoryAndDescription(t *testing.T) {
	r := posestest.New(t)
	r.Install(t)
	all, err := r.Lib.Poses()
	require.NoError(t, err)

	cases := map[string]struct {
		q    poses.Query
		want []string
	}{
		"nothing asked":          {poses.Query{}, []string{"P001", "P002", "U001"}},
		"a Japanese name":        {poses.Query{Text: "腕を組"}, []string{"P002"}},
		"an English name, cased": {poses.Query{Text: "SITTING"}, []string{"P001"}},
		"the description":        {poses.Query{Text: "heart"}, []string{"U001"}},
		"the number":             {poses.Query{Text: "p002"}, []string{"P002"}},
		"every word must match":  {poses.Query{Text: "sitting floor"}, []string{"P001"}},
		"a word that is not":     {poses.Query{Text: "sitting standing"}, nil},
		"a series":               {poses.Query{Series: "upper"}, []string{"U001"}},
		"a category":             {poses.Query{Class: "立ち"}, []string{"P002"}},
		"series and category":    {poses.Query{Series: "upper", Class: "立ち"}, nil},
	}
	for name, tc := range cases {
		assert.Equal(t, tc.want, ids(poses.Filter(all, tc.q)), name)
	}
	assert.Equal(t, []string{"座り", "立ち", "かわいい系"}, poses.Classes(all, ""))
	assert.Equal(t, []string{"かわいい系"}, poses.Classes(all, "upper"))
}
