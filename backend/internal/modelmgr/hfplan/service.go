package hfplan

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

// maxQuery bounds a search keyword.
const maxQuery = 200

// Search runs a search after checking the input (an empty or long keyword, a kind that is not one of hf.PipelineTags is a
// *Refusal). Hits tagged not-for-all-audiences are left out unless adultOK (the age gate is open).
func Search(ctx context.Context, c *hf.Client, query, kind string, limit int, adultOK bool) ([]hf.SearchHit, error) {
	q := strings.TrimSpace(query)
	switch {
	case q == "" || len(q) > maxQuery:
		return nil, refusal("検索のキーワードは 1〜200 文字で入れてください")
	case kind != "" && !slices.Contains(hf.PipelineTags, kind):
		return nil, refusal("種別は %s のどれかにしてください", strings.Join(hf.PipelineTags, "・"))
	}
	hits, err := c.Search(ctx, hf.SearchOptions{Query: q, PipelineTag: kind, Limit: limit})
	if err != nil {
		return nil, explain(err, "", c.HasToken())
	}
	if !adultOK {
		hits = slices.DeleteFunc(hits, func(h hf.SearchHit) bool { return h.NFAA })
	}
	return hits, nil
}

// VRAMOf is the video memory of the largest GPU in bytes (0 when none is known).
func VRAMOf(adapters []gpu.Adapter) (n uint64) {
	for _, a := range adapters {
		if !a.Software {
			n = max(n, a.VRAMBytes)
		}
	}
	return n
}

// Message is the text of an error that explains itself to the user (a *Refusal, a *NameTakenError, a bad input), and
// adult is true for the closed age gate. ok is false for any other error.
func Message(err error) (msg string, adult, ok bool) {
	var rf *Refusal
	var taken *NameTakenError
	switch {
	case errors.Is(err, ErrAdult):
		return "", true, true
	case errors.As(err, &rf):
		return rf.Message, false, true
	case errors.As(err, &taken):
		return taken.Message, false, true
	case errors.Is(err, hf.ErrInput):
		return strings.TrimPrefix(err.Error(), hf.ErrInput.Error()+": "), false, true
	}
	return "", false, false
}

// Record is the user model an installed payload is recorded as.
func Record(p Payload, family string, workflows []string, now time.Time) store.UserModel {
	return store.UserModel{
		Path: p.Rel(), Repo: p.Repo, Commit: p.Commit, RepoPath: p.Path, Folder: p.Folder, Name: p.Name, SHA256: p.SHA256, Size: p.Size,
		License: p.StoreLicense(), Gated: p.Gated, NFAA: p.NFAA, Family: family, Workflows: workflows, InstalledAt: now.UTC(),
	}
}

// KeepEarlier is rec with what the record of the same bytes at the same path already says: a file that was recorded as adult
// stays adult when the same bytes are installed again from a repository that is not tagged, and the workflows made for it stay.
// A record of other bytes is not what rec replaces, so rec is returned as it is.
func KeepEarlier(rec, old store.UserModel) store.UserModel {
	if old.SHA256 != rec.SHA256 || old.Path != rec.Path {
		return rec
	}
	rec.NFAA = rec.NFAA || old.NFAA
	rec.Workflows = slices.Concat(rec.Workflows, old.Workflows)
	slices.Sort(rec.Workflows)
	rec.Workflows = slices.Compact(rec.Workflows)
	return rec
}

// StoreLicense is the license of the payload as state.json records it.
func (p Payload) StoreLicense() store.HFLicense {
	return store.HFLicense{ID: p.License.ID, Name: p.License.Name, Link: p.License.Link}
}

// UserModelView is a file the user added from Hugging Face, for the list.
type UserModelView struct {
	store.UserModel
	// ModelIDs are the #831 identifiers of the models created for it (an SDXL checkpoint: text to image and reference to image).
	ModelIDs []string `json:"model_ids"`
	// Present: the file is in place with its size. SharedWith lists the catalog models that hold the same file: it stays while
	// one of them is installed.
	Present    bool     `json:"present"`
	SharedWith []string `json:"shared_with,omitempty"`
}

// UserModels lists the user models newest first. A file of a repository for adults is left out unless adultOK. present and
// holders say whether the file is in place and which installed catalog models hold it.
func UserModels(st store.State, adultOK bool, endpoint string, present func(store.UserModel) bool, holders func(path string) []string) []UserModelView {
	out := make([]UserModelView, 0, len(st.UserModels))
	for _, um := range st.UserModels {
		if um.NFAA && !adultOK {
			continue
		}
		v := UserModelView{UserModel: um, ModelIDs: []string{}, Present: present(um), SharedWith: holders(um.Path)}
		for _, w := range um.Workflows {
			v.ModelIDs = append(v.ModelIDs, localcfg.ID(localcfg.KindComfyUI, endpoint, w))
		}
		out = append(out, v)
	}
	slices.SortFunc(out, func(a, b UserModelView) int {
		if c := b.InstalledAt.Compare(a.InstalledAt); c != 0 {
			return c
		}
		return strings.Compare(a.Path, b.Path)
	})
	return out
}
