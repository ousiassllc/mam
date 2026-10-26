package modelmgr

import (
	"net/url"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// IDs of the elements of the import from Hugging Face.
const (
	HFImportID = "mm-hfi"
	HFResultID = "mm-hfi-result"
	HFModelsID = "mm-hfi-models"
	HFMsgID    = "mm-hfi-msg"
)

// Names of the form fields of the import (the handler reads the same names).
const (
	FieldQuery  = "q"
	FieldKind   = "kind"
	FieldRepo   = "repo"
	FieldCommit = "commit"
	FieldPath   = "path"
	FieldFolder = "folder"
	FieldName   = "name"
	FieldTerms  = "terms_version"
)

// Limits of the fields of the import: the keyword or URL (what the service accepts, and a URL is longer than a keyword) and a
// file name.
const (
	MaxQueryLen    = 512
	MaxFileNameLen = 120
)

// HFKinds are the choices of the search filter: the value of pipeline_tag and its label ("" for all).
var HFKinds = [][2]string{{"", "すべて"}, {"text-to-image", "テキストから画像"}, {"text-to-video", "テキストから動画"}, {"image-to-video", "画像から動画"}}

// HFImport is the section "Hugging Face から導入": the search form, what it found, and the files the user added.
type HFImport struct {
	Query  string
	Kind   string
	Result HFResult
	Models HFModelList
}

// HFHit is a repository of a search result.
type HFHit struct {
	Repo      string
	Downloads string
	Likes     string
	Gated     bool
	NFAA      bool
}

// HFFile is a file of a repository: its facts, whether it can be installed, and the form that installs it.
type HFFile struct {
	Path        string
	Name        string
	Size        string
	SHA256      string
	Format      string
	Installable bool
	// Reason says why the file cannot be installed.
	Reason string
	// Security is the Hub's scan ("safe", "caution", "unsafe", "" when not read).
	Security string
	Variant  string
	// Folder is the folder to start from ("" when unknown: the user chooses); FolderNote says how it was guessed.
	Folder     string
	FolderNote string
	Family     string
	// Fit is the VRAM mark: fits, tight, over or unknown; Need is the estimate as text.
	Fit       string
	Need      string
	Installed bool
	Selected  bool
	// Job is the install of this file that is queued or running.
	Job string
}

// HFGroup is the versions of one model (by file name), shown together.
type HFGroup struct {
	Name  string
	Files []HFFile
}

// HFRepo is a repository at its pinned commit.
type HFRepo struct {
	ID          string
	Commit      string
	Page        string
	Gated       bool
	NFAA        bool
	License     string
	LicenseWarn string
	// Terms and TermsVersion are what the user confirms before an install; Consented says that was done for this commit.
	Terms        string
	TermsVersion string
	Consented    bool
	NoToken      bool
	Truncated    bool
	Folders      []string
	Groups       []HFGroup
}

// HFResult is the answer to a search or a repository: a message, the hits or the repository.
type HFResult struct {
	Message
	Hits []HFHit
	Repo *HFRepo
}

// HFJob is an import that is queued or running.
type HFJob struct {
	Name     string
	Progress Progress
}

// HFModel is a file the user added from Hugging Face.
type HFModel struct {
	Path     string
	Name     string
	Folder   string
	Repo     string
	Commit   string
	Size     string
	License  string
	Gated    bool
	NFAA     bool
	Family   string
	ModelIDs []string
	Shared   []string
	Missing  bool
	// At is when it was added, as text.
	At string
}

// HFModelList is what the user added, with the imports that are running.
type HFModelList struct {
	Message
	Installing []HFJob
	Models     []HFModel
	// Jobs are the import jobs the list reports on (running, or just ended).
	Jobs []string
	// OOB draws the list out of band (a polled list that ended an install also redraws the trial choices).
	OOB bool
}

// Polling reports whether an import is running (or has not been seen to end yet).
func (l HFModelList) Polling() bool { return len(l.Jobs) > 0 }

func hfSearchURL(repo string) string {
	return paths.ModelManagerPart("hf") + "?" + FieldQuery + "=" + url.QueryEscape(repo)
}

// hfModelsURL is where the list asks for itself again: with the jobs it reports on.
func hfModelsURL(l HFModelList) string {
	return paths.ModelManagerPart("hf", "models") + "?jobs=" + url.QueryEscape(strings.Join(l.Jobs, ","))
}

func fitLabel(fit string) string {
	switch fit {
	case "fits":
		return "収まる見込み"
	case "tight":
		return "ぎりぎり"
	case "over":
		return "収まらない見込み"
	}
	return "VRAM 不明"
}

func securityLabel(s string) string {
	switch s {
	case "safe":
		return "スキャン: 問題なし"
	case "caution":
		return "スキャン: 注意"
	case "unsafe":
		return "スキャン: unsafe"
	}
	return "スキャン: 未取得"
}

func queryEscape(s string) string { return url.QueryEscape(s) }

// shortHash is the first characters of a commit hash, as the screen writes it.
func shortHash(h string) string { return h[:min(len(h), 12)] }
