package modelmanager

import (
	"context"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hfplan"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

// The import from Hugging Face (spec/architecture/model-manager/model-manager.md §13): the reads, the decisions and the pins
// are package hfplan's; this file is the Service's part (the age gate, the token, the record, the job).

// HFRepoView is a repository at a pinned commit (hfplan.RepoView).
type HFRepoView = hfplan.RepoView

// HFInstallInput names one file of a repository at a commit. Folder and Name override what mam decides ("" to keep it). Accept
// is the user's confirmation of the terms (TermsVersion is the one they were shown, hfplan.TermsVersion); it is recorded for
// the repository and commit, so a second file of it needs none.
type HFInstallInput struct {
	Repo, Commit, Path string
	Folder, Name       string
	Accept             bool
	TermsVersion       string
}

// SearchHF searches Hugging Face, most downloaded first; kind is "" or one of hf.PipelineTags. Repositories tagged
// not-for-all-audiences are left out while the age gate is closed.
func (s *Service) SearchHF(ctx context.Context, query, kind string, limit int) ([]hf.SearchHit, error) {
	hits, err := hfplan.Search(ctx, s.hf, query, kind, limit, s.AdultContentEnabled())
	return hits, s.hfError(err)
}

// HFRepo resolves what the user typed (owner/repo or a huggingface.co URL), pins the repository to a commit and lists its model
// files with size, SHA-256, format, whether each can be installed and why not, the scan status, the folder mam would use and
// a VRAM mark. A repository tagged not-for-all-audiences is ErrAdultNotConfirmed while the age gate is closed.
func (s *Service) HFRepo(ctx context.Context, input string) (HFRepoView, error) {
	ref, err := hf.ParseInput(input)
	if err != nil {
		return HFRepoView{}, s.hfError(err)
	}
	l, err := hfplan.Browse(ctx, s.hf, ref, hfplan.BrowseOptions{AdultOK: s.AdultContentEnabled(), HasToken: s.hf.HasToken(), VRAM: hfplan.VRAMOf(s.GPU(ctx).Adapters)})
	if err != nil {
		return HFRepoView{}, s.hfError(err)
	}
	st := s.state.Get()
	k := hfplan.Known{
		SHA256: map[string]bool{}, HasToken: s.hf.HasToken(), Consented: hfConsented(st, l.Repo.ID, l.Repo.Commit),
		Job: func(path string) string { return s.activeJob(hfplan.Target(l.Repo.ID, l.Repo.Commit, path)) },
	}
	for _, um := range st.UserModels {
		k.SHA256[um.SHA256] = true
	}
	return hfplan.MakeView(l, k), nil
}

func hfConsented(st store.State, repo, commit string) bool {
	c, ok := st.HFConsents[hfplan.ConsentKey(repo, commit)]
	return ok && c.TermsVersion == hfplan.TermsVersion
}

// hfError turns what a Hugging Face read said into the Service's refusals: a message the user can act on is an *InvalidError,
// the closed age gate is ErrAdultNotConfirmed; anything else (the network) is logged and returned with a hint.
func (s *Service) hfError(err error) error {
	if err == nil {
		return nil
	}
	if msg, adult, ok := hfplan.Message(err); adult {
		return ErrAdultNotConfirmed
	} else if ok {
		return invalid("%s", msg)
	}
	s.Logger.Warn("hugging face request failed", "err", err)
	return fmt.Errorf("hugging face: ネットワークを確かめてからやり直してください: %w", err)
}

// InstallHFFile installs one .safetensors file of a Hugging Face repository as a job (JobStatus; resumable like a catalog
// install). It re-reads what the download is checked against (the commit's size, LFS SHA-256 and scan) from Hugging Face and
// refuses at once, before any download: a file that cannot be installed, a repository for adults without the age gate, a
// gated one without a token or without access, an unknown folder, a name taken by a different file (*InvalidError, another
// name suggested), the terms not confirmed (ErrConsentRequired), no engine, a pinned settings file, a disk without the room.
func (s *Service) InstallHFFile(ctx context.Context, in HFInstallInput) (JobRef, error) {
	if !s.bundled {
		return JobRef{}, ErrUnsupported
	}
	if !s.engine.Status().Installed {
		return JobRef{}, ErrEngineNotInstalled
	}
	if view, err := s.Models.View(); err != nil {
		return JobRef{}, err
	} else if view.Pinned {
		return JobRef{}, ErrPinned
	}
	p, err := hfplan.Pin(ctx, s.hf, in.Repo, in.Commit, in.Path, hfplan.PinOptions{
		AdultOK: s.AdultContentEnabled(), HasToken: s.hf.HasToken(), Folder: in.Folder, Name: in.Name,
	})
	if err != nil {
		return JobRef{}, s.hfError(err)
	}
	if p.Folder == "" {
		return JobRef{}, invalid("導入先のフォルダを決められませんでした。folder に %s のどれかを指定してください", hfplan.JoinFolders())
	}
	pl := hfplan.PayloadOf(p)
	checks := []func() error{ // in this order: the terms are recorded last, once nothing else refuses
		func() error { return s.hfError(hfplan.CheckDest(s.Layout.ModelPath(pl.Rel()), pl, s.recordedAt)) },
		s.Layout.CheckWritable, func() error { return s.checkHFSpace(pl) }, func() error { return s.acceptHFTerms(in, pl) },
	}
	for _, check := range checks {
		if err := check(); err != nil {
			return JobRef{}, userFacing(ctx, err)
		}
	}
	return s.enqueue(ctx, pl.Target(), installPayload{Target: pl.Target(), HF: &pl})
}

// acceptHFTerms records the confirmation the call carries and refuses when the terms were not confirmed for the repository
// and commit (the API enforces it, not the screen).
func (s *Service) acceptHFTerms(in HFInstallInput, p hfplan.Payload) error {
	switch {
	case !in.Accept && hfConsented(s.state.Get(), p.Repo, p.Commit):
		return nil
	case !in.Accept:
		return ErrConsentRequired
	case in.TermsVersion != hfplan.TermsVersion:
		return invalid("確認した注意書きの版が違います（%s が必要です）。画面を読み込み直してください", hfplan.TermsVersion)
	}
	return s.state.Update(func(st *store.State) {
		if st.HFConsents == nil {
			st.HFConsents = map[string]store.HFConsent{}
		}
		st.HFConsents[hfplan.ConsentKey(p.Repo, p.Commit)] = store.HFConsent{TermsVersion: hfplan.TermsVersion, License: p.StoreLicense(), AcceptedAt: s.Now().UTC()}
	})
}

// recordedAt is what an install recorded for the path (a catalog model's file or a user model).
func (s *Service) recordedAt(rel string) (sha string, size int64, ok bool) {
	st := s.state.Get()
	for _, im := range st.Models {
		for _, f := range im.Files {
			if strings.EqualFold(f.Path, rel) {
				return f.SHA256, f.Size, true
			}
		}
	}
	for _, um := range st.UserModels {
		if strings.EqualFold(um.Path, rel) {
			return um.SHA256, um.Size, true
		}
	}
	return "", 0, false
}

func (s *Service) checkHFSpace(p hfplan.Payload) error {
	dest := s.Layout.ModelPath(p.Rel())
	if need := p.Size - partSize(dest, p.Size); !fileIsThere(dest, p.Size) && need > 0 {
		if have := s.free(s.Layout.DataDir()); uint64(need) > have {
			return &fetch.NoSpaceError{Need: uint64(need), Free: have, Path: s.Layout.DataDir()}
		}
	}
	return nil
}
