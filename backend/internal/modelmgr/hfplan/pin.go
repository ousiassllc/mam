package hfplan

import (
	"context"
	"net/http"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf"
)

// PinOptions are what Pin needs besides the file.
type PinOptions struct {
	// AdultOK is the age gate. HasToken says a Hugging Face token is stored.
	AdultOK, HasToken bool
	// Folder and Name are the user's choice ("" to decide here).
	Folder, Name string
}

// Pinned is a file fixed to a commit with everything a download is checked against, read from the Hub again (nothing
// the caller said about the size or the SHA-256 is trusted).
type Pinned struct {
	// Repo is the canonical "owner/name" (a renamed repository is followed).
	Repo   string `json:"repo"`
	Commit string `json:"commit"`
	Path   string `json:"path"`
	// URL is the commit-pinned resolve URL the download uses.
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	Security string `json:"security,omitempty"`
	// Folder is where it goes ("" when it could not be decided: the caller asks the user) and FolderSource says how
	// (SourceUser, SourcePath, SourceHeader). Name is the file name there.
	Folder       string     `json:"folder"`
	FolderSource string     `json:"folder_source,omitempty"`
	FolderReason string     `json:"folder_reason,omitempty"`
	Family       string     `json:"family,omitempty"`
	Name         string     `json:"name"`
	Gated        bool       `json:"gated"`
	NFAA         bool       `json:"nfaa"`
	License      hf.License `json:"license"`
	Page         string     `json:"page"`
}

// Pin resolves the file of the repository at the commit (a full 40-digit hash) and checks it can be installed: the repository
// is open (the age gate for an NFAA one, a stored token for a gated one, access granted to the token), the file exists
// and is an installable .safetensors with an LFS SHA-256, and the Hub's scan does not call it unsafe. It then decides the
// folder (the user's, the repository path's, or the header's) and the file name. Nothing is downloaded but a first byte
// of a gated file and, when the folder has to be guessed, the header.
func Pin(ctx context.Context, c *hf.Client, repo, commit, path string, o PinOptions) (Pinned, error) {
	switch {
	case !hf.ValidRepo(repo) || !hf.ValidPath(path):
		return Pinned{}, refusal("リポジトリまたはファイルのパスが正しくありません")
	case !hf.IsCommit(commit):
		return Pinned{}, refusal("コミットは 40 桁のハッシュで指定してください（get_hf_model_files の commit）")
	case o.Folder != "" && !ValidFolder(o.Folder):
		return Pinned{}, refusal("導入先のフォルダは %s のどれかにしてください", strings.Join(Folders, "・"))
	case o.Name != "" && !ValidName(o.Name):
		return Pinned{}, refusal("ファイル名は英数字と . _ + - だけの .safetensors（または .sft）にしてください（120 文字まで）")
	}
	r, err := c.Revision(ctx, repo, commit)
	if err != nil {
		return Pinned{}, explain(err, repo, o.HasToken)
	}
	switch {
	case r.Commit != commit:
		return Pinned{}, refusal("コミット %s は %s にありません", commit, repo)
	case r.Disabled:
		return Pinned{}, refusal("%s は Hugging Face が無効にしています", r.ID)
	case r.NFAA && !o.AdultOK:
		return Pinned{}, ErrAdult
	}
	if r.Gated && !o.HasToken {
		return Pinned{}, noToken(r.ID, c.PageURL(r.ID))
	}
	infos, err := c.PathsInfo(ctx, r.ID, commit, []string{path})
	if r.Gated && err != nil && unauthorized(err) { // a gated repository answers 401 to paths-info until access is granted
		return Pinned{}, noAccess(r.ID, c.PageURL(r.ID))
	}
	if err != nil {
		return Pinned{}, explain(err, r.ID, o.HasToken)
	}
	fi, ok := infos[path]
	if !ok {
		return Pinned{}, refusal("%s のコミット %s に %s がありません", r.ID, commit[:8], path)
	}
	entry := hf.TreeEntry{Path: path, Size: fi.Size, SHA256: fi.SHA256, LFSSize: fi.LFSSize}
	if v := Judge(entry, fi.Security); !v.Installable {
		return Pinned{}, refusal("導入できません: %s", v.Reason)
	}
	p := Pinned{
		Repo: r.ID, Commit: commit, Path: path, URL: c.ResolveURL(r.ID, commit, path), SHA256: fi.SHA256, Size: fi.Size,
		Security: fi.Security, Gated: r.Gated, NFAA: r.NFAA, License: r.License, Page: c.PageURL(r.ID),
	}
	if err := checkAccess(ctx, c, p, o); err != nil {
		return Pinned{}, err
	}
	p.Name = o.Name
	if p.Name == "" {
		p.Name = SanitizeName(path)
	}
	decideFolder(ctx, c, &p, o)
	return p, nil
}

// checkAccess asks the Hub for the first byte of a gated file: a token that was not granted access is refused here, before any
// download (access to a gated repository is requested on its page, never through the API).
func checkAccess(ctx context.Context, c *hf.Client, p Pinned, o PinOptions) error {
	if !p.Gated {
		return nil
	}
	err := c.Probe(ctx, p.Repo, p.Commit, p.Path)
	if err != nil && unauthorized(err) {
		return noAccess(p.Repo, p.Page)
	}
	return explain(err, p.Repo, o.HasToken)
}

func unauthorized(err error) bool {
	return hf.IsStatus(err, http.StatusUnauthorized, http.StatusForbidden)
}

func noToken(repo, page string) error {
	return refusal("%s は gated（アクセスの承認が要る）リポジトリです。Hugging Face のトークンを設定し、%s でアクセスを申請してください", repo, page)
}

func noAccess(repo, page string) error {
	return refusal("このトークンでは %s を取得できません。Hugging Face のページ（%s）でアクセスを申請し、承認されてからやり直してください（mam からは申請できません）", repo, page)
}

// decideFolder sets the folder: the user's, else the repository path's, else the guess from the header (a failure to read
// it leaves the folder empty).
func decideFolder(ctx context.Context, c *hf.Client, p *Pinned, o PinOptions) {
	if o.Folder != "" {
		p.Folder, p.FolderSource = o.Folder, SourceUser
		return
	}
	if f := FolderFromPath(p.Path); f != "" {
		p.Folder, p.FolderSource = f, SourcePath
		return
	}
	h, err := c.Header(ctx, p.Repo, p.Commit, p.Path)
	if err != nil {
		return
	}
	inf := Infer(h)
	p.Folder, p.FolderSource, p.FolderReason, p.Family = inf.Folder, SourceHeader, inf.Reason, inf.Family
	if p.Folder == "" {
		p.FolderSource = ""
	}
}
