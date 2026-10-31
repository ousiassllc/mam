package hfplan_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf/hftest"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hfplan"
)

const commit = "0123456789abcdef0123456789abcdef01234567"

func client(srv *hftest.Server, token string) *hf.Client {
	anyURL := func(*url.URL) error { return nil }
	return hf.New(hf.Options{
		Base: srv.URL, CheckURL: anyURL, HTTP: fetch.WithRedirectRules(&http.Client{}, anyURL, "127.0.0.1"), AuthHost: "127.0.0.1",
		Token: func() string { return token }, Wait: func(int, time.Duration) time.Duration { return 0 },
	})
}

var (
	sdxlBody  = hftest.Safetensors([]string{"model.diffusion_model.a", "conditioner.embedders.1.model.b", "first_stage_model.c"}, nil, 1000)
	loraBody  = hftest.Safetensors([]string{"lora_unet_a.lora_down.weight", "lora_unet_a.alpha"}, nil, 100)
	vaeBody   = hftest.Safetensors([]string{"encoder.x", "decoder.y", "quant_conv.z"}, nil, 100)
	unknownFB = hftest.Safetensors([]string{"foo.bar"}, nil, 100)
)

func repo() hftest.Repo {
	return hftest.Repo{ID: "o/m", Commit: commit, License: "openrail++", Files: []hftest.File{
		{Path: "README.md", Body: []byte("# hi")},
		{Path: "pony-fp16.safetensors", Body: sdxlBody},
		{Path: "pony-bf16.safetensors", Body: append(append([]byte{}, sdxlBody...), 1)},
		{Path: "split_files/vae/ae.safetensors", Body: vaeBody},
		{Path: "style-lora.safetensors", Body: loraBody},
		{Path: "mystery.safetensors", Body: unknownFB},
		{Path: "model.Q4_K_M.gguf", Body: []byte("GGUF....")},
		{Path: "old/model.ckpt", Body: []byte("pickle")},
		{Path: "unet/diffusion_pytorch_model.safetensors", Body: sdxlBody},
		{Path: "plain.safetensors", Body: sdxlBody, NoLFS: true},
		{Path: "bad.safetensors", Body: sdxlBody, Security: "unsafe"},
	}}
}

func byPath(l hfplan.Listing) map[string]hfplan.File {
	m := map[string]hfplan.File{}
	for _, f := range l.Files {
		m[f.Path] = f
	}
	return m
}

func TestBrowse_ListsTheModelFilesWithVerdictFolderAndSecurity(t *testing.T) {
	srv := hftest.New(t, repo())
	l, err := hfplan.Browse(context.Background(), client(srv, ""), hf.Ref{Repo: "o/m", Kind: hf.KindRepo}, hfplan.BrowseOptions{VRAM: 8 << 30})
	require.NoError(t, err)
	assert.Equal(t, commit, l.Repo.Commit)
	assert.Equal(t, "openrail++", l.Repo.License.ID)
	assert.Equal(t, srv.URL+"/o/m", l.Page)
	files := byPath(l)
	assert.NotContains(t, files, "README.md", "a file that is not a model file is not listed")
	assert.Len(t, files, 10)

	sdxl := files["pony-fp16.safetensors"]
	assert.True(t, sdxl.Installable)
	assert.Equal(t, hftest.SHA(sdxlBody), sdxl.SHA256)
	assert.Equal(t, int64(len(sdxlBody)), sdxl.Size)
	assert.Equal(t, "checkpoints", sdxl.Folder)
	assert.Equal(t, hfplan.SourceHeader, sdxl.FolderSource)
	assert.Equal(t, hfplan.FamilySDXL, sdxl.Family)
	assert.Equal(t, "pony", sdxl.Group)
	assert.Equal(t, "fp16", sdxl.Variant)
	assert.Equal(t, "bf16", files["pony-bf16.safetensors"].Variant)
	assert.Equal(t, hfplan.FitFits, sdxl.Fit)

	ae := files["split_files/vae/ae.safetensors"]
	assert.Equal(t, hfplan.File{}.Folder+"vae", ae.Folder)
	assert.Equal(t, hfplan.SourcePath, ae.FolderSource, "the path decides before the header is read")
	assert.Equal(t, "loras", files["style-lora.safetensors"].Folder)
	assert.Empty(t, files["mystery.safetensors"].Folder, "an unknown layout is left to the user")
	assert.True(t, files["mystery.safetensors"].Installable)

	gguf := files["model.Q4_K_M.gguf"]
	assert.False(t, gguf.Installable)
	assert.Equal(t, hfplan.ReasonGGUF, gguf.Reason)
	assert.Equal(t, hfplan.ReasonPickle, files["old/model.ckpt"].Reason)
	assert.Equal(t, hfplan.ReasonSplit, files["unet/diffusion_pytorch_model.safetensors"].Reason)
	assert.Equal(t, hfplan.ReasonNoLFS, files["plain.safetensors"].Reason)
	assert.Equal(t, hfplan.ReasonUnsafe, files["bad.safetensors"].Reason)
	assert.Equal(t, "unsafe", files["bad.safetensors"].Security)
	assert.Equal(t, "safe", sdxl.Security)

	for i := 1; i < len(l.Files); i++ {
		assert.LessOrEqual(t, l.Files[i-1].Group, l.Files[i].Group, "versions of one model are listed together")
	}
}

func TestBrowse_AFileThatDoesNotFitTheGPUIsMarked(t *testing.T) {
	srv := hftest.New(t, repo())
	l, err := hfplan.Browse(context.Background(), client(srv, ""), hf.Ref{Repo: "o/m", Kind: hf.KindRepo}, hfplan.BrowseOptions{VRAM: 1000})
	require.NoError(t, err)
	assert.Equal(t, hfplan.FitOver, byPath(l)["pony-fp16.safetensors"].Fit)
	l, err = hfplan.Browse(context.Background(), client(srv, ""), hf.Ref{Repo: "o/m", Kind: hf.KindRepo}, hfplan.BrowseOptions{})
	require.NoError(t, err)
	assert.Equal(t, hfplan.FitUnknown, byPath(l)["pony-fp16.safetensors"].Fit, "no GPU found")
}

func TestBrowse_ATreeURLNarrowsToTheDirectoryAndABlobURLMarksTheFile(t *testing.T) {
	srv := hftest.New(t, repo())
	c := client(srv, "")
	l, err := hfplan.Browse(context.Background(), c, hf.Ref{Repo: "o/m", Kind: hf.KindTree, Rev: "main", Path: "split_files"}, hfplan.BrowseOptions{})
	require.NoError(t, err)
	require.Len(t, l.Files, 1)
	assert.Equal(t, "split_files/vae/ae.safetensors", l.Files[0].Path)

	l, err = hfplan.Browse(context.Background(), c, hf.Ref{Repo: "o/m", Kind: hf.KindBlob, Rev: commit, Path: "style-lora.safetensors"}, hfplan.BrowseOptions{})
	require.NoError(t, err)
	assert.Len(t, l.Files, 10)
	for _, f := range l.Files {
		assert.Equal(t, f.Path == "style-lora.safetensors", f.Selected, f.Path)
	}

	_, err = hfplan.Browse(context.Background(), c, hf.Ref{Repo: "o/m", Kind: hf.KindResolve, Rev: "main", Path: "missing.safetensors"}, hfplan.BrowseOptions{})
	var rf *hfplan.Refusal
	assert.ErrorAs(t, err, &rf)
}

func TestBrowse_ARenamedRepositoryIsFollowed(t *testing.T) {
	srv := hftest.New(t, repo())
	srv.Rename("o/old", "o/m")
	l, err := hfplan.Browse(context.Background(), client(srv, ""), hf.Ref{Repo: "o/old", Kind: hf.KindRepo}, hfplan.BrowseOptions{})
	require.NoError(t, err)
	assert.Equal(t, "o/m", l.Repo.ID)
}

func TestBrowse_AnNFAARepositoryIsRefusedWithoutTheAgeGate(t *testing.T) {
	r := repo()
	r.NFAA = true
	srv := hftest.New(t, r)
	_, err := hfplan.Browse(context.Background(), client(srv, ""), hf.Ref{Repo: "o/m", Kind: hf.KindRepo}, hfplan.BrowseOptions{})
	require.ErrorIs(t, err, hfplan.ErrAdult)
	assert.Zero(t, srv.Count("/api/models/o/m/tree/"), "nothing of the repository is read once it is known to be adult")

	l, err := hfplan.Browse(context.Background(), client(srv, ""), hf.Ref{Repo: "o/m", Kind: hf.KindRepo}, hfplan.BrowseOptions{AdultOK: true})
	require.NoError(t, err)
	assert.True(t, l.Repo.NFAA)
}

// An older commit of a repository that is NFAA today may predate the tag: naming it must not open the gate.
const oldCommit = "fedcba9876543210fedcba9876543210fedcba98"

func TestBrowse_AnOlderCommitWithoutTheTagIsStillAdultWhileTheHeadIsTagged(t *testing.T) {
	r := repo()
	r.NFAA, r.OldCommit = true, oldCommit
	srv := hftest.New(t, r)
	for _, ref := range []hf.Ref{
		{Repo: "o/m", Kind: hf.KindTree, Rev: oldCommit},
		{Repo: "o/m", Kind: hf.KindBlob, Rev: oldCommit, Path: "pony-fp16.safetensors"},
		{Repo: "o/m", Kind: hf.KindResolve, Rev: oldCommit, Path: "pony-fp16.safetensors"},
	} {
		_, err := hfplan.Browse(context.Background(), client(srv, ""), ref, hfplan.BrowseOptions{})
		require.ErrorIs(t, err, hfplan.ErrAdult, ref.Kind)
	}
	assert.Zero(t, srv.Count("/api/models/o/m/tree/"), "nothing of the repository is read")

	l, err := hfplan.Browse(context.Background(), client(srv, ""), hf.Ref{Repo: "o/m", Kind: hf.KindTree, Rev: oldCommit}, hfplan.BrowseOptions{AdultOK: true})
	require.NoError(t, err)
	assert.True(t, l.Repo.NFAA)
	assert.Equal(t, oldCommit, l.Repo.Commit)
}

func TestBrowse_MissingAndRateLimitedRepositoriesSayWhatToDo(t *testing.T) {
	srv := hftest.New(t, repo())
	_, err := hfplan.Browse(context.Background(), client(srv, ""), hf.Ref{Repo: "o/none", Kind: hf.KindRepo}, hfplan.BrowseOptions{})
	var rf *hfplan.Refusal
	require.ErrorAs(t, err, &rf)
	assert.Contains(t, rf.Message, "o/none")
	assert.Contains(t, rf.Message, "トークン")

	srv.RateLimit(1000)
	_, err = hfplan.Browse(context.Background(), client(srv, ""), hf.Ref{Repo: "o/m", Kind: hf.KindRepo}, hfplan.BrowseOptions{})
	require.ErrorAs(t, err, &rf)
	assert.Contains(t, rf.Message, "レート制限")
	assert.Contains(t, rf.Message, "トークン")
}

func pin(t *testing.T, srv *hftest.Server, token, path string, o hfplan.PinOptions) (hfplan.Pinned, error) {
	t.Helper()
	o.HasToken = token != ""
	return hfplan.Pin(context.Background(), client(srv, token), "o/m", commit, path, o)
}

func TestPin_ReadsThePinsFromTheHubAndDecidesFolderAndName(t *testing.T) {
	srv := hftest.New(t, repo())
	p, err := pin(t, srv, "", "pony-fp16.safetensors", hfplan.PinOptions{})
	require.NoError(t, err)
	assert.Equal(t, hfplan.Pinned{
		Repo: "o/m", Commit: commit, Path: "pony-fp16.safetensors", URL: srv.URL + "/o/m/resolve/" + commit + "/pony-fp16.safetensors",
		SHA256: hftest.SHA(sdxlBody), Size: int64(len(sdxlBody)), Security: "safe", Folder: "checkpoints", FolderSource: hfplan.SourceHeader,
		FolderReason: p.FolderReason, Family: hfplan.FamilySDXL, Name: "pony-fp16.safetensors", License: hf.License{ID: "openrail++"}, Page: srv.URL + "/o/m",
	}, p)
	assert.NotEmpty(t, p.FolderReason)

	p, err = pin(t, srv, "", "split_files/vae/ae.safetensors", hfplan.PinOptions{})
	require.NoError(t, err)
	assert.Equal(t, "vae", p.Folder)
	assert.Equal(t, hfplan.SourcePath, p.FolderSource)
	assert.Equal(t, "ae.safetensors", p.Name)

	p, err = pin(t, srv, "", "split_files/vae/ae.safetensors", hfplan.PinOptions{Folder: "loras", Name: "mine.sft"})
	require.NoError(t, err)
	assert.Equal(t, "loras", p.Folder, "the user's folder wins over the path")
	assert.Equal(t, hfplan.SourceUser, p.FolderSource)
	assert.Equal(t, "mine.sft", p.Name)

	p, err = pin(t, srv, "", "mystery.safetensors", hfplan.PinOptions{})
	require.NoError(t, err)
	assert.Empty(t, p.Folder, "the caller asks the user")
}

func TestPin_RefusesWhatCannotBeInstalled(t *testing.T) {
	srv := hftest.New(t, repo())
	for path, want := range map[string]string{
		"model.Q4_K_M.gguf":                        hfplan.ReasonGGUF,
		"old/model.ckpt":                           hfplan.ReasonPickle,
		"unet/diffusion_pytorch_model.safetensors": hfplan.ReasonSplit,
		"plain.safetensors":                        hfplan.ReasonNoLFS,
		"bad.safetensors":                          hfplan.ReasonUnsafe,
		"README.md":                                hfplan.ReasonNotModel,
	} {
		_, err := pin(t, srv, "", path, hfplan.PinOptions{})
		var rf *hfplan.Refusal
		require.ErrorAs(t, err, &rf, path)
		assert.Contains(t, rf.Message, want, path)
	}
	for _, bad := range []struct {
		path string
		o    hfplan.PinOptions
	}{
		{"missing.safetensors", hfplan.PinOptions{}},
		{"../x.safetensors", hfplan.PinOptions{}},
		{"pony-fp16.safetensors", hfplan.PinOptions{Folder: "../../etc"}},
		{"pony-fp16.safetensors", hfplan.PinOptions{Name: "../x.safetensors"}},
		{"pony-fp16.safetensors", hfplan.PinOptions{Name: "x.ckpt"}},
	} {
		_, err := pin(t, srv, "", bad.path, bad.o)
		var rf *hfplan.Refusal
		assert.ErrorAs(t, err, &rf, "%v", bad)
	}
	var rf *hfplan.Refusal
	_, err := hfplan.Pin(context.Background(), client(srv, ""), "o/m", "main", "pony-fp16.safetensors", hfplan.PinOptions{})
	assert.ErrorAs(t, err, &rf, "a branch is not a commit")
	_, err = hfplan.Pin(context.Background(), client(srv, ""), "o/m", "ffffffffffffffffffffffffffffffffffffffff", "pony-fp16.safetensors", hfplan.PinOptions{})
	assert.Error(t, err, "a commit the repository does not have")
}

func TestPin_AGatedRepositoryStopsBeforeAnyDownload(t *testing.T) {
	r := repo()
	r.Gated = true
	srv := hftest.New(t, r)

	_, err := pin(t, srv, "", "pony-fp16.safetensors", hfplan.PinOptions{})
	var rf *hfplan.Refusal
	require.ErrorAs(t, err, &rf)
	assert.Contains(t, rf.Message, "gated")
	assert.Contains(t, rf.Message, "トークン")
	assert.Contains(t, rf.Message, srv.URL+"/o/m", "the link to the page")

	_, err = pin(t, srv, srv.Token, "pony-fp16.safetensors", hfplan.PinOptions{})
	require.ErrorAs(t, err, &rf, "a token that has no access yet")
	assert.Contains(t, rf.Message, "アクセスを申請")
	assert.Contains(t, rf.Message, srv.URL+"/o/m")

	srv.Granted.Store(true)
	p, err := pin(t, srv, srv.Token, "pony-fp16.safetensors", hfplan.PinOptions{})
	require.NoError(t, err)
	assert.True(t, p.Gated)
}

func TestPin_AnNFAARepositoryIsRefusedWithoutTheAgeGate(t *testing.T) {
	r := repo()
	r.NFAA = true
	srv := hftest.New(t, r)
	_, err := pin(t, srv, "", "pony-fp16.safetensors", hfplan.PinOptions{})
	require.ErrorIs(t, err, hfplan.ErrAdult)
	p, err := pin(t, srv, "", "pony-fp16.safetensors", hfplan.PinOptions{AdultOK: true})
	require.NoError(t, err)
	assert.True(t, p.NFAA)
}

func TestPin_AnOlderCommitWithoutTheTagIsStillAdultWhileTheHeadIsTagged(t *testing.T) {
	r := repo()
	r.NFAA, r.OldCommit = true, oldCommit
	srv := hftest.New(t, r)
	pinOld := func(o hfplan.PinOptions) (hfplan.Pinned, error) {
		return hfplan.Pin(context.Background(), client(srv, ""), "o/m", oldCommit, "pony-fp16.safetensors", o)
	}
	_, err := pinOld(hfplan.PinOptions{})
	require.ErrorIs(t, err, hfplan.ErrAdult)
	assert.Zero(t, srv.Count("/api/models/o/m/paths-info/"), "nothing of the file is read")

	p, err := pinOld(hfplan.PinOptions{AdultOK: true})
	require.NoError(t, err)
	assert.True(t, p.NFAA, "the file is recorded as adult")
	assert.Equal(t, oldCommit, p.Commit)
}
