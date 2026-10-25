package hfplan

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf"
)

// The terms of an import. A change of the wording is a new TermsVersion: the confirmation of the old one no longer counts.
const (
	TermsVersion = "1"
	Terms        = "取得するのはあなたの判断です。mam は、このファイルの中身・安全性・権利関係を確認していません。上のライセンスと取得元を自分で確かめたうえで導入してください。"
	// LicenseWarning is shown when the model card gives no usable license.
	LicenseWarning = "ライセンスの記載がない（または other で名前も無い）リポジトリです。使えるか、商用利用してよいかを、リポジトリのページで確かめてください"
)

// FileView is a file of a repository: what Browse found, and whether the user has it already.
type FileView struct {
	File
	// Installed: a user model with the same SHA-256 is recorded. InstallJob is the install that is queued or running.
	Installed  bool   `json:"installed"`
	InstallJob string `json:"install_job,omitempty"`
}

// RepoView is a repository at a pinned commit, as the import screen and get_hf_model_files show it.
type RepoView struct {
	Repo      hf.Repo    `json:"repo"`
	Page      string     `json:"page"`
	Files     []FileView `json:"files"`
	Truncated bool       `json:"truncated,omitempty"`
	// Selected is the file a blob or resolve URL pointed at.
	Selected string `json:"selected,omitempty"`
	// LicenseWarning says the card gives no usable license ("" when it does).
	LicenseWarning string `json:"license_warning,omitempty"`
	// Terms and TermsVersion are what the user has to confirm; Consented says they were for this repository and commit.
	Terms        string `json:"terms"`
	TermsVersion string `json:"terms_version"`
	Consented    bool   `json:"consented"`
	// TokenConfigured: a Hugging Face token is stored (a gated repository needs one).
	TokenConfigured bool `json:"token_configured"`
}

// Known is what the model manager already knows of the user's files and of this repository.
type Known struct {
	// SHA256 are the SHA-256 of the user models that are recorded.
	SHA256 map[string]bool
	// Job returns the install job of a file that is queued or running ("" for none).
	Job                 func(path string) string
	Consented, HasToken bool
}

// MakeView is the listing with what the model manager knows added.
func MakeView(l Listing, k Known) RepoView {
	v := RepoView{
		Repo: l.Repo, Page: l.Page, Truncated: l.Truncated, Terms: Terms, TermsVersion: TermsVersion,
		Consented: k.Consented, TokenConfigured: k.HasToken, Files: make([]FileView, 0, len(l.Files)),
	}
	if l.Repo.License.Unclear() {
		v.LicenseWarning = LicenseWarning
	}
	for _, f := range l.Files {
		fv := FileView{File: f, Installed: f.SHA256 != "" && k.SHA256[f.SHA256]}
		if k.Job != nil {
			fv.InstallJob = k.Job(f.Path)
		}
		if f.Selected {
			v.Selected = f.Path
		}
		v.Files = append(v.Files, fv)
	}
	return v
}

// Target is the job target of a file: "hf:<owner>/<repo>@<commit>/<path>".
func Target(repo, commit, path string) string { return "hf:" + repo + "@" + commit + "/" + path }

// ConsentKey is the key of the confirmation of the terms: "<repo>@<commit>".
func ConsentKey(repo, commit string) string { return repo + "@" + commit }

// Payload is what an import job downloads: the pins read from Hugging Face when it was queued.
type Payload struct {
	Repo    string     `json:"repo"`
	Commit  string     `json:"commit"`
	Path    string     `json:"path"`
	URL     string     `json:"url"`
	Folder  string     `json:"folder"`
	Name    string     `json:"name"`
	SHA256  string     `json:"sha256"`
	Size    int64      `json:"size"`
	License hf.License `json:"license"`
	Gated   bool       `json:"gated,omitempty"`
	NFAA    bool       `json:"nfaa,omitempty"`
}

// PayloadOf is the payload of a pinned file.
func PayloadOf(p Pinned) Payload {
	return Payload{
		Repo: p.Repo, Commit: p.Commit, Path: p.Path, URL: p.URL, Folder: p.Folder, Name: p.Name, SHA256: p.SHA256, Size: p.Size,
		License: p.License, Gated: p.Gated, NFAA: p.NFAA,
	}
}

// Rel is where the file lies below the data directory: models/<folder>/<name>.
func (p Payload) Rel() string { return "models/" + p.Folder + "/" + p.Name }

// Target is the job target of the payload.
func (p Payload) Target() string { return Target(p.Repo, p.Commit, p.Path) }

// NameTakenError is a destination that holds a different file: the install never replaces it.
type NameTakenError struct{ Message string }

func (e *NameTakenError) Error() string { return e.Message }

// CheckDest refuses to replace a different file at dest: it is free, or it holds the same file (same SHA-256 and size, as
// recorded by an earlier install — recorded returns it for the relative path — or, for a file nobody recorded, as hashed
// now). Anything else is a *NameTakenError that suggests another name.
func CheckDest(dest string, p Payload, recorded func(rel string) (sha string, size int64, ok bool)) error {
	st, err := os.Stat(dest)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	taken := &NameTakenError{Message: p.Rel() + " には別のファイルがあります。上書きしません。name に別の名前（例: " + SuggestName(p.Name, p.SHA256) + "）を指定するか、中止してください"}
	if sha, size, known := recorded(p.Rel()); known {
		if sha == p.SHA256 && size == p.Size {
			return nil
		}
		return taken
	}
	if st.Size() != p.Size {
		return taken
	}
	if same, err := fetch.Verified(dest, fetch.Source{URL: p.URL, SHA256: p.SHA256, Size: p.Size}); err != nil {
		return err
	} else if !same {
		return taken
	}
	return nil
}

// ModelWriter creates or replaces a model of the bundled endpoint by name.
type ModelWriter func(ctx context.Context, name string, m localcfg.Model) error

// RegisterCheckpoint creates the text-to-image and reference-to-image models of an SDXL checkpoint: the SDXL templates with
// the checkpoint name swapped for the file, named by WorkflowNames. It returns their names.
func RegisterCheckpoint(ctx context.Context, write ModelWriter, p Payload) ([]string, error) {
	txt, img := WorkflowNames(p.Name, p.SHA256)
	names := []string{txt, img}
	for i, id := range []string{TemplateTxt2Img, TemplateImg2Img} {
		tpl, ok := localcfg.TemplateByID(id)
		if !ok {
			return nil, errors.New("hfplan: the template " + id + " does not exist")
		}
		m, err := WithCheckpoint(tpl, p.Name)
		if err != nil {
			return nil, err
		}
		if err := write(ctx, names[i], m); err != nil {
			return nil, err
		}
	}
	return names, nil
}

// FamilyOfFile reads the model family from the header of a file on disk ("" when it cannot be read or is not known).
func FamilyOfFile(path string) string {
	h, err := hf.ReadFileHeader(path)
	if err != nil {
		return ""
	}
	return Infer(h).Family
}

// JoinFolders lists the folders an install can use, for a message.
func JoinFolders() string { return strings.Join(Folders, "・") }
