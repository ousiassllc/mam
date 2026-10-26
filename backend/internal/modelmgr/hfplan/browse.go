package hfplan

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf"
)

const (
	// maxListed bounds the files shown for a repository.
	maxListed = 300
	// maxScanned bounds the files whose security status is asked for in one listing.
	maxScanned = 200
	// maxHeaderReads bounds the files whose header is read to guess the folder, and headerParallel how many are read at once.
	maxHeaderReads = 12
	headerParallel = 4
	headerTimeout  = 20 * time.Second
)

// File is a file of a repository as the list shows it.
type File struct {
	Path string `json:"path"`
	// Name is the file name an install would give it (see SanitizeName).
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256,omitempty"`
	Format Format `json:"format"`
	// Installable is false for a file that cannot be installed; Reason says why.
	Installable bool   `json:"installable"`
	Reason      string `json:"reason,omitempty"`
	// Security is the status of the Hub's scan ("safe", "caution", "unsafe"; "" when not read).
	Security string `json:"security,omitempty"`
	// Group and Variant sort the versions of one model together ("flux1-dev" and "fp8_e4m3fn").
	Group   string `json:"group"`
	Variant string `json:"variant,omitempty"`
	// Folder is the guessed folder ("" when unknown: the user chooses); FolderSource is "path" or "header". Family is the
	// model family read from the header ("sdxl", ...).
	Folder       string `json:"folder,omitempty"`
	FolderSource string `json:"folder_source,omitempty"`
	Family       string `json:"family,omitempty"`
	FolderReason string `json:"folder_reason,omitempty"`
	// Fit says whether the weights are likely to fit the GPU (FitFits ...); NeedBytes is the estimate.
	Fit       string `json:"fit"`
	NeedBytes int64  `json:"need_bytes"`
	// Selected marks the file a blob or resolve URL pointed at.
	Selected bool `json:"selected,omitempty"`
}

// Listing is a repository at a commit with its model files.
type Listing struct {
	Repo hf.Repo `json:"repo"`
	// Page is the repository's page on the Hub (license, access request).
	Page  string `json:"page"`
	Files []File `json:"files"`
	// Truncated: the repository has more files than the list holds.
	Truncated bool `json:"truncated,omitempty"`
}

// BrowseOptions are what Browse needs to know about the user.
type BrowseOptions struct {
	// AdultOK is the age gate (an NFAA repository is ErrAdult without it).
	AdultOK  bool
	HasToken bool
	// VRAM is the video memory of the largest GPU in bytes (0: unknown).
	VRAM uint64
}

// Browse pins the repository named by ref to a commit and lists its model files with size, SHA-256, format, verdict, scan
// status, folder guess and a VRAM mark. A tree URL narrows the list to the directory; a blob or resolve URL marks its file.
func Browse(ctx context.Context, c *hf.Client, ref hf.Ref, o BrowseOptions) (Listing, error) {
	repo, err := c.Revision(ctx, ref.Repo, ref.Rev)
	if err != nil {
		return Listing{}, explain(err, ref.Repo, o.HasToken)
	}
	if repo.Disabled {
		return Listing{}, refusal("%s は Hugging Face が無効にしています", repo.ID)
	}
	if repo.NFAA && !o.AdultOK {
		return Listing{}, ErrAdult
	}
	entries, truncated, err := c.Tree(ctx, repo.ID, repo.Commit)
	if err != nil {
		return Listing{}, explain(err, repo.ID, o.HasToken)
	}
	entries = narrow(entries, ref)
	if ref.Kind == hf.KindBlob || ref.Kind == hf.KindResolve {
		if !slices.ContainsFunc(entries, func(e hf.TreeEntry) bool { return e.Path == ref.Path }) {
			return Listing{}, refusal("%s にファイル %s がありません", repo.ID, ref.Path)
		}
	}
	l := Listing{Repo: repo, Page: c.PageURL(repo.ID), Truncated: truncated}
	var models []hf.TreeEntry
	for _, e := range entries {
		if FormatOf(e.Path) != FormatOther {
			models = append(models, e)
		}
	}
	if len(models) > maxListed {
		models, l.Truncated = models[:maxListed], true
	}
	security := scanStatus(ctx, c, repo, models)
	for _, e := range models {
		l.Files = append(l.Files, describe(e, security[e.Path], o.VRAM, ref))
	}
	slices.SortStableFunc(l.Files, func(a, b File) int {
		if x := strings.Compare(a.Group, b.Group); x != 0 {
			return x
		}
		if x := strings.Compare(a.Variant, b.Variant); x != 0 {
			return x
		}
		return strings.Compare(a.Path, b.Path)
	})
	guessFolders(ctx, c, repo, l.Files)
	return l, nil
}

// narrow keeps what the input points at: the directory of a tree URL (everything for the other forms).
func narrow(entries []hf.TreeEntry, ref hf.Ref) []hf.TreeEntry {
	if ref.Kind != hf.KindTree || ref.Path == "" {
		return entries
	}
	prefix := strings.TrimSuffix(ref.Path, "/") + "/"
	return slices.DeleteFunc(slices.Clone(entries), func(e hf.TreeEntry) bool { return !strings.HasPrefix(e.Path, prefix) })
}

// scanStatus asks the Hub for the security status of the safetensors files (the others are not installable anyway). A scan
// that cannot be read leaves the status empty: the install asks again and refuses an unsafe file then.
func scanStatus(ctx context.Context, c *hf.Client, repo hf.Repo, entries []hf.TreeEntry) map[string]string {
	var paths []string
	for _, e := range entries {
		if FormatOf(e.Path) == FormatSafetensors && len(paths) < maxScanned {
			paths = append(paths, e.Path)
		}
	}
	out := map[string]string{}
	if len(paths) == 0 {
		return out
	}
	infos, err := c.PathsInfo(ctx, repo.ID, repo.Commit, paths)
	if err != nil {
		return out
	}
	for p, fi := range infos {
		out[p] = fi.Security
	}
	return out
}

func describe(e hf.TreeEntry, security string, vram uint64, ref hf.Ref) File {
	v := Judge(e, security)
	group, variant := Group(e.Path)
	f := File{
		Path: e.Path, Name: SanitizeName(e.Path), Size: e.Size, SHA256: e.SHA256, Format: FormatOf(e.Path),
		Installable: v.Installable, Reason: v.Reason, Security: security, Group: group, Variant: variant,
		Selected: (ref.Kind == hf.KindBlob || ref.Kind == hf.KindResolve) && e.Path == ref.Path,
	}
	if f.Installable {
		f.Fit, f.NeedBytes = Fit(e.Size, vram)
		if folder := FolderFromPath(e.Path); folder != "" {
			f.Folder, f.FolderSource = folder, SourcePath
		}
	} else {
		f.Fit = FitUnknown
	}
	return f
}

// guessFolders reads the header of the installable files whose folder the path does not name (a few at a time, the first
// maxHeaderReads of them) and sets the folder and family from the tensor names. A header that cannot be read leaves the folder
// empty: the user chooses.
func guessFolders(ctx context.Context, c *hf.Client, repo hf.Repo, files []File) {
	var todo []int
	for i, f := range files {
		if f.Installable && f.Folder == "" && len(todo) < maxHeaderReads {
			todo = append(todo, i)
		}
	}
	sem := make(chan struct{}, headerParallel)
	var wg sync.WaitGroup
	for _, i := range todo {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			ctx, cancel := context.WithTimeout(ctx, headerTimeout)
			defer cancel()
			h, err := c.Header(ctx, repo.ID, repo.Commit, files[i].Path)
			if err != nil {
				return
			}
			if inf := Infer(h); inf.Folder != "" {
				files[i].Folder, files[i].FolderSource, files[i].FolderReason = inf.Folder, SourceHeader, inf.Reason
				files[i].Family = inf.Family
			}
		}()
	}
	wg.Wait()
}
