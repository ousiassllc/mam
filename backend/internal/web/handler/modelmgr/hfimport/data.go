package hfimport

import (
	"context"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hfplan"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/jobview"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
)

// shortCommit is the first characters of a commit hash, as the screen writes it.
func shortCommit(commit string) string { return commit[:min(len(commit), 12)] }

// licenseText is the license of a repository as one line ("記載なし" when its card names none).
func licenseText(l hf.License) string {
	switch {
	case l.ID != "" && l.Name != "":
		return l.ID + "（" + l.Name + "）"
	case l.ID != "":
		return l.ID
	case l.Name != "":
		return l.Name
	}
	return "記載なし"
}

// fileName is the file name of the job target "hf:<repo>@<commit>/<path in the repository>".
func fileName(target string) string {
	_, rest, _ := strings.Cut(target, "@")
	_, rel, _ := strings.Cut(rest, "/")
	if rel == "" {
		return "モデルのファイル"
	}
	return path.Base(rel)
}

// RepoOf is a repository at its commit as the screen draws it.
func RepoOf(v modelmanager.HFRepoView) view.HFRepo {
	r := view.HFRepo{
		ID: v.Repo.ID, Commit: v.Repo.Commit, Page: v.Page, Gated: v.Repo.Gated, NFAA: v.Repo.NFAA,
		License: licenseText(v.Repo.License), LicenseWarn: v.LicenseWarning, Terms: v.Terms, TermsVersion: v.TermsVersion,
		Consented: v.Consented, NoToken: !v.TokenConfigured, Truncated: v.Truncated, Folders: hfplan.Folders,
	}
	groups := map[string]int{}
	for _, f := range v.Files {
		i, ok := groups[f.Group]
		if !ok {
			i = len(r.Groups)
			groups[f.Group] = i
			r.Groups = append(r.Groups, view.HFGroup{Name: f.Group})
		}
		r.Groups[i].Files = append(r.Groups[i].Files, fileOf(f))
	}
	return r
}

func fileOf(f hfplan.FileView) view.HFFile {
	sha := f.SHA256
	if len(sha) > 16 {
		sha = sha[:16] + "…"
	}
	return view.HFFile{
		Path: f.Path, Name: f.Name, Size: jobview.BytesText(f.Size), SHA256: sha, Format: string(f.Format), Installable: f.Installable,
		Reason: f.Reason, Security: f.Security, Variant: f.Variant, Folder: f.Folder, FolderNote: f.FolderReason, Family: f.Family,
		Fit: f.Fit, Need: jobview.BytesText(f.NeedBytes), Installed: f.Installed, Selected: f.Selected, Job: f.InstallJob,
	}
}

// HitsOf is the repositories of a search as the screen draws them.
func HitsOf(hits []hf.SearchHit) []view.HFHit {
	out := make([]view.HFHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, view.HFHit{Repo: h.Repo, Downloads: strconv.FormatInt(h.Downloads, 10), Likes: strconv.FormatInt(h.Likes, 10), Gated: h.Gated, NFAA: h.NFAA})
	}
	return out
}

// ListData is the files the user added and the imports that run. jobs are the ones a poll reports on: one that ended is
// said once, one the queue has not recorded the end of yet keeps the list polling.
func ListData(ctx context.Context, svc *modelmanager.Service, jobs []string) view.HFModelList {
	var l view.HFModelList
	active := svc.ActiveHFJobs()
	targets := make([]string, 0, len(active))
	for t := range active {
		targets = append(targets, t)
	}
	sort.Strings(targets)
	running := map[string]bool{}
	for _, t := range targets {
		running[active[t]] = true
		l.Jobs = append(l.Jobs, active[t])
		l.Installing = append(l.Installing, view.HFJob{Name: fileName(t), Progress: *jobview.Progress(ctx, svc, active[t])})
	}
	var notices, failures []string
	for _, id := range jobs {
		st, err := svc.JobStatus(ctx, id)
		if running[id] || err != nil || !strings.HasPrefix(st.Target, "hf:") {
			continue
		}
		if st.State == domain.JobQueued || st.State == domain.JobRunning {
			l.Jobs = append(l.Jobs, id) // the end is not recorded yet: ask again
			l.Installing = append(l.Installing, view.HFJob{Name: fileName(st.Target), Progress: *jobview.Progress(ctx, svc, id)})
			continue
		}
		if n, f := jobview.Ended(ctx, svc, id, "「"+fileName(st.Target)+"」"); n != "" {
			notices = append(notices, n)
		} else if f != "" {
			failures = append(failures, f)
		}
	}
	l.Notice, l.Error = strings.Join(notices, "。"), strings.Join(failures, "\n")
	for _, um := range svc.UserModels() {
		l.Models = append(l.Models, modelOf(um))
	}
	return l
}

func modelOf(um modelmanager.UserModelView) view.HFModel {
	return view.HFModel{
		Path: um.Path, Name: um.Name, Folder: um.Folder, Repo: um.Repo, Commit: shortCommit(um.Commit), Size: jobview.BytesText(um.Size),
		License: licenseText(hf.License{ID: um.License.ID, Name: um.License.Name}), Gated: um.Gated, NFAA: um.NFAA, Family: um.Family,
		ModelIDs: um.ModelIDs, Shared: slices.Clone(um.SharedWith), Missing: !um.Present, At: um.InstalledAt.Local().Format(time.DateTime),
	}
}
