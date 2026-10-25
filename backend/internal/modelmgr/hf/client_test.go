package hf_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf/hftest"
)

const commit = "0123456789abcdef0123456789abcdef01234567"

// newClient is a client of the fake: the loopback host is allowed and is the one host that gets the token.
func newClient(srv *hftest.Server, token string) *hf.Client {
	anyURL := func(*url.URL) error { return nil }
	return hf.New(hf.Options{
		Base: srv.URL, CheckURL: anyURL, HTTP: fetch.WithRedirectRules(&http.Client{}, anyURL, "127.0.0.1"), AuthHost: "127.0.0.1",
		Token: func() string { return token }, Wait: func(int, time.Duration) time.Duration { return 0 },
	})
}

func files() []hftest.File {
	return []hftest.File{
		{Path: "README.md", Body: []byte("# readme")},
		{Path: "a.safetensors", Body: hftest.Safetensors([]string{"x"}, nil, 100)},
		{Path: "dir/b.safetensors", Body: hftest.Safetensors([]string{"y"}, nil, 50)},
		{Path: "c.gguf", Body: []byte("GGUF")},
		{Path: "d.safetensors", Body: []byte("plain git file"), NoLFS: true},
	}
}

func TestRevision_PinsTheCommitAndReadsLicenseAndTags(t *testing.T) {
	srv := hftest.New(t, hftest.Repo{ID: "o/m", Commit: commit, License: "other", LicenseName: "my-license", LicenseLink: "LICENSE.md", NFAA: true, Gated: true})
	r, err := newClient(srv, "").Revision(context.Background(), "o/m", "")
	require.NoError(t, err)
	assert.Equal(t, hf.Repo{
		ID: "o/m", Commit: commit, Gated: true, NFAA: true, Tags: []string{"safetensors", "not-for-all-audiences"},
		License: hf.License{ID: "other", Name: "my-license", Link: "LICENSE.md"},
	}, r)
	assert.False(t, r.License.Unclear())
	assert.True(t, hf.License{ID: "other"}.Unclear())
	assert.True(t, hf.License{}.Unclear())
}

func TestRevision_FollowsARenamedRepositoryAndReportsTheNewName(t *testing.T) {
	srv := hftest.New(t, hftest.Repo{ID: "o/new", Commit: commit})
	srv.Rename("o/old", "o/new")
	r, err := newClient(srv, "").Revision(context.Background(), "o/old", "main")
	require.NoError(t, err)
	assert.Equal(t, "o/new", r.ID)
}

func TestRevision_MissingAndPrivateRepositoriesAreAPIErrors(t *testing.T) {
	srv := hftest.New(t, hftest.Repo{ID: "o/m", Commit: commit})
	_, err := newClient(srv, "").Revision(context.Background(), "o/none", "main")
	assert.True(t, hf.IsStatus(err, http.StatusUnauthorized))
	_, err = newClient(srv, "").Revision(context.Background(), "o/m", "nope")
	assert.True(t, hf.IsStatus(err, http.StatusNotFound))
}

func TestTree_FollowsThePagesAndKeepsTheLFSPins(t *testing.T) {
	srv := hftest.New(t, hftest.Repo{ID: "o/m", Commit: commit, Files: files()})
	entries, truncated, err := newClient(srv, "").Tree(context.Background(), "o/m", commit)
	require.NoError(t, err)
	assert.False(t, truncated)
	require.Len(t, entries, 5)
	byPath := map[string]hf.TreeEntry{}
	for _, e := range entries {
		byPath[e.Path] = e
	}
	a := files()[1]
	assert.Equal(t, hf.TreeEntry{Path: "a.safetensors", Size: int64(len(a.Body)), SHA256: hftest.SHA(a.Body), LFSSize: int64(len(a.Body))}, byPath["a.safetensors"])
	assert.Empty(t, byPath["d.safetensors"].SHA256, "a file that is not in LFS has no SHA-256")
	assert.Equal(t, 3, srv.Count("/api/models/o/m/tree/"), "three pages of two")
	for _, r := range srv.Requests() {
		assert.NotEqual(t, "huggingface.co", r.Host, "the next page is built from the client's base, never from the Link header")
	}
}

func TestPathsInfo_ReadsTheScanStatus(t *testing.T) {
	fs := files()
	fs[1].Security = "unsafe"
	srv := hftest.New(t, hftest.Repo{ID: "o/m", Commit: commit, Files: fs})
	infos, err := newClient(srv, "").PathsInfo(context.Background(), "o/m", commit, []string{"a.safetensors", "dir/b.safetensors", "missing.safetensors"})
	require.NoError(t, err)
	assert.Equal(t, "unsafe", infos["a.safetensors"].Security)
	assert.Equal(t, "safe", infos["dir/b.safetensors"].Security)
	assert.Equal(t, hftest.SHA(fs[1].Body), infos["a.safetensors"].SHA256)
	assert.NotContains(t, infos, "missing.safetensors")
}

func TestHeader_ReadsTheTensorNamesWithTwoSmallRanges(t *testing.T) {
	body := hftest.Safetensors([]string{"double_blocks.0.x", "img_in.weight"}, map[string]string{"modelspec.architecture": "flux-1"}, 1_000_000)
	srv := hftest.New(t, hftest.Repo{ID: "o/m", Commit: commit, Files: []hftest.File{{Path: "w.safetensors", Body: body}}})
	h, err := newClient(srv, "").Header(context.Background(), "o/m", commit, "w.safetensors")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"double_blocks.0.x", "img_in.weight"}, h.Names)
	assert.Equal(t, "flux-1", h.Metadata["modelspec.architecture"])
	cdn := 0
	for _, r := range srv.Requests() {
		if r.Host == srv.CDNHost() {
			cdn++
			assert.NotEmpty(t, r.Range, "the weights are never fetched whole")
		}
	}
	assert.Equal(t, 2, cdn, "the length, then the JSON")
}

func TestHeader_RefusesAFileThatIsNotSafetensors(t *testing.T) {
	srv := hftest.New(t, hftest.Repo{ID: "o/m", Commit: commit, Files: []hftest.File{
		{Path: "bad.safetensors", Body: []byte("GGUF this is not a safetensors file at all, padded to more than eight bytes")},
	}})
	_, err := newClient(srv, "").Header(context.Background(), "o/m", commit, "bad.safetensors")
	assert.ErrorIs(t, err, hf.ErrHeader)
}

func TestSearch_ListsHitsByDownloadsAndMarksGatedAndNFAA(t *testing.T) {
	srv := hftest.New(t,
		hftest.Repo{ID: "o/sdxl-small", Commit: commit, Downloads: 5, PipelineTag: "text-to-image"},
		hftest.Repo{ID: "o/sdxl-big", Commit: commit, Downloads: 500, Likes: 7, Gated: true, PipelineTag: "text-to-image"},
		hftest.Repo{ID: "o/sdxl-nsfw", Commit: commit, Downloads: 50, NFAA: true, PipelineTag: "text-to-image"},
		hftest.Repo{ID: "o/sdxl-video", Commit: commit, Downloads: 1, PipelineTag: "text-to-video"},
	)
	hits, err := newClient(srv, "").Search(context.Background(), hf.SearchOptions{Query: "sdxl", PipelineTag: "text-to-image"})
	require.NoError(t, err)
	require.Len(t, hits, 3)
	assert.Equal(t, "o/sdxl-big", hits[0].Repo)
	assert.True(t, hits[0].Gated)
	assert.Equal(t, int64(7), hits[0].Likes)
	assert.Equal(t, "o/sdxl-nsfw", hits[1].Repo)
	assert.True(t, hits[1].NFAA)
	assert.False(t, hits[2].NFAA)
}

func TestRateLimit_IsRepeatedThenReported(t *testing.T) {
	srv := hftest.New(t, hftest.Repo{ID: "o/m", Commit: commit})
	srv.RateLimit(2)
	_, err := newClient(srv, "").Revision(context.Background(), "o/m", "main")
	require.NoError(t, err, "two 429s are waited out")

	before := srv.Count("/api/models/o/m/revision/")
	srv.RateLimit(100)
	_, err = newClient(srv, "").Revision(context.Background(), "o/m", "main")
	var rl *hf.RateLimitError
	require.True(t, errors.As(err, &rl))
	assert.Equal(t, 4, srv.Count("/api/models/o/m/revision/")-before, "one try and three repeats, no more")
}

func TestToken_IsSentToTheAPIHostOnlyNotToTheCDN(t *testing.T) {
	body := hftest.Safetensors([]string{"x"}, nil, 10)
	srv := hftest.New(t, hftest.Repo{ID: "o/m", Commit: commit, Gated: true, Files: []hftest.File{{Path: "w.safetensors", Body: body}}})
	srv.Granted = true
	c := newClient(srv, srv.Token)
	require.NoError(t, c.Probe(context.Background(), "o/m", commit, "w.safetensors"))
	var api, cdn int
	for _, r := range srv.Requests() {
		switch {
		case r.Host == srv.CDNHost():
			cdn++
			assert.Empty(t, r.Auth, "the CDN host never sees the token: %s", r.Path)
		default:
			api++
			assert.Equal(t, "Bearer "+srv.Token, r.Auth, r.Path)
		}
	}
	assert.Equal(t, 1, api)
	assert.Equal(t, 1, cdn, "the 302 was followed to the CDN")
}

func TestToken_IsNotSentWhenTheHostIsNotTheAuthHost(t *testing.T) {
	srv := hftest.New(t, hftest.Repo{ID: "o/m", Commit: commit})
	anyURL := func(*url.URL) error { return nil }
	c := hf.New(hf.Options{Base: srv.URL, CheckURL: anyURL, HTTP: &http.Client{}, Token: func() string { return "hf_secret" }}) // AuthHost defaults to huggingface.co
	_, err := c.Revision(context.Background(), "o/m", "main")
	require.NoError(t, err)
	for _, r := range srv.Requests() {
		assert.Empty(t, r.Auth)
	}
}

func TestProbe_AGatedRepositoryWithoutAccessIs401Or403(t *testing.T) {
	srv := hftest.New(t, hftest.Repo{ID: "o/m", Commit: commit, Gated: true, Files: []hftest.File{{Path: "w.safetensors", Body: []byte("12345678")}}})
	err := newClient(srv, "").Probe(context.Background(), "o/m", commit, "w.safetensors")
	require.True(t, hf.IsStatus(err, http.StatusUnauthorized))
	var ae *hf.APIError
	require.True(t, errors.As(err, &ae))
	assert.Equal(t, "GatedRepo", ae.Code)
	err = newClient(srv, srv.Token).Probe(context.Background(), "o/m", commit, "w.safetensors")
	assert.True(t, hf.IsStatus(err, http.StatusForbidden), "a token without access")
}

func TestClient_RefusesAHostOutsideTheAllowList(t *testing.T) {
	c := hf.New(hf.Options{Base: "https://evil.example"})
	_, err := c.Revision(context.Background(), "o/m", "main")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a download host")
}

func TestResolveURL_IsPinnedToTheCommit(t *testing.T) {
	c := hf.New(hf.Options{})
	assert.Equal(t, "https://huggingface.co/o/m/resolve/"+commit+"/split_files/x%20y.safetensors", c.ResolveURL("o/m", commit, "split_files/x y.safetensors"))
}
