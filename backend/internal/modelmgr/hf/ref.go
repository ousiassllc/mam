// Package hf reads Hugging Face for the model manager's import (spec/architecture/model-manager/model-manager.md §13):
// it resolves what the user typed (owner/repo or a huggingface.co URL), searches, lists the files of a repository at a
// commit with their sizes, LFS SHA-256 and security scan, reads the header of a .safetensors file to guess the ComfyUI
// folder, and judges which files can be installed. It never downloads a model file: the install passes the pinned URL,
// SHA-256 and size to package fetch.
//
// Every request goes through the allow-list of package fetch (huggingface.co and *.hf.co over https, no address of the
// local network) and the token is sent to huggingface.co only.
package hf

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

// Kind is the form of an input: a bare repository, a tree (directory), a blob (file page) or a resolve (file link).
type Kind string

// The forms ParseInput accepts.
const (
	KindRepo    Kind = "repo"
	KindTree    Kind = "tree"
	KindBlob    Kind = "blob"
	KindResolve Kind = "resolve"
)

// Ref is a repository, optionally at a revision and a path inside it.
type Ref struct {
	// Repo is "owner/name".
	Repo string `json:"repo"`
	// Rev is a branch, tag or commit ("" for the default branch).
	Rev string `json:"rev,omitempty"`
	// Path is the file (blob, resolve) or directory (tree) the input points at ("" for the whole repository).
	Path string `json:"path,omitempty"`
	Kind Kind   `json:"kind"`
}

// maxInput bounds what ParseInput reads.
const maxInput = 2048

var (
	repoPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,95}/[A-Za-z0-9][A-Za-z0-9._-]{0,95}$`)
	revPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	commitRe    = regexp.MustCompile(`^[0-9a-f]{40}$`)
	// reservedOwners are the first path segments of huggingface.co that are not a model owner (datasets/<owner>/<name>, spaces/...).
	reservedOwners = map[string]bool{
		"datasets": true, "spaces": true, "models": true, "api": true, "docs": true, "blog": true, "papers": true,
		"collections": true, "settings": true, "join": true, "login": true, "organizations": true, "new": true,
	}
)

// ErrInput is a refused input; its message says what to type instead (shown to the user).
var ErrInput = errors.New("hf: unusable input")

func inputError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInput, fmt.Sprintf(format, args...))
}

// IsCommit reports whether rev is a full commit hash (40 lower-case hex digits).
func IsCommit(rev string) bool { return commitRe.MatchString(rev) }

// ValidRepo reports whether s is "owner/name".
func ValidRepo(s string) bool {
	owner, _, _ := strings.Cut(s, "/")
	return repoPattern.MatchString(s) && !reservedOwners[strings.ToLower(owner)]
}

// ValidRev reports whether rev can be used in a URL path as a branch, tag or commit.
func ValidRev(rev string) bool { return revPattern.MatchString(rev) }

// ValidPath reports whether p is a path inside a repository that is safe to put in a URL and to take a file name from:
// relative, with no empty, "." or ".." segment, no backslash and no control character.
func ValidPath(p string) bool {
	if p == "" || len(p) > 1024 {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return !strings.ContainsFunc(p, func(r rune) bool { return r == '\\' || r == '?' || r == '#' || unicode.IsControl(r) })
}

// ParseInput resolves what the user typed: "owner/repo", "https://huggingface.co/owner/repo", ".../tree/<rev>[/path]",
// ".../blob/<rev>/<path>" or ".../resolve/<rev>/<path>". Anything else is ErrInput: another host (including the hf.co
// short URL), http, a user name or port in the URL, a query string or a fragment.
func ParseInput(s string) (Ref, error) {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return Ref{}, inputError("リポジトリ ID（owner/repo）または Hugging Face の URL を入れてください")
	case len(s) > maxInput || strings.ContainsFunc(s, unicode.IsControl):
		return Ref{}, inputError("入力が長すぎるか、使えない文字を含んでいます")
	case strings.Contains(s, "://"):
		return parseURL(s)
	}
	if !ValidRepo(s) {
		return Ref{}, inputError("リポジトリ ID は owner/repo の形で入れてください（例: stabilityai/stable-diffusion-xl-base-1.0）")
	}
	return Ref{Repo: s, Kind: KindRepo}, nil
}

func parseURL(s string) (Ref, error) {
	u, err := url.Parse(s)
	switch {
	case err != nil:
		return Ref{}, inputError("URL を読めません")
	case u.Scheme != "https":
		return Ref{}, inputError("https:// の URL だけ使えます")
	case u.User != nil || u.Port() != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "":
		return Ref{}, inputError("URL にユーザー名・ポート・?・# は付けられません。リポジトリのページの URL をそのまま貼ってください")
	case !strings.EqualFold(strings.TrimSuffix(u.Hostname(), "."), "huggingface.co"):
		return Ref{}, inputError("取得できるのは huggingface.co の URL だけです（hf.co の短縮 URL は使えません。huggingface.co の URL か owner/repo を入れてください）")
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segs) < 2 || !ValidRepo(segs[0]+"/"+segs[1]) {
		return Ref{}, inputError("モデルのリポジトリの URL ではありません（datasets・spaces などは使えません）")
	}
	ref := Ref{Repo: segs[0] + "/" + segs[1], Kind: KindRepo}
	rest := segs[2:]
	if len(rest) == 0 {
		return ref, nil
	}
	switch Kind(rest[0]) {
	case KindTree, KindBlob, KindResolve:
		ref.Kind = Kind(rest[0])
	default:
		return Ref{}, inputError("リポジトリのページの URL（/tree/…・/blob/…・/resolve/…）だけ使えます")
	}
	if len(rest) < 2 || !ValidRev(rest[1]) {
		return Ref{}, inputError("URL のリビジョン（ブランチ名またはコミット）を読めません")
	}
	ref.Rev = rest[1]
	if len(rest) > 2 {
		ref.Path = strings.Join(rest[2:], "/")
		if !ValidPath(ref.Path) {
			return Ref{}, inputError("URL のパスを使えません")
		}
	}
	if ref.Kind != KindTree && ref.Path == "" {
		return Ref{}, inputError("ファイルの URL にはパスが要ります")
	}
	return ref, nil
}
