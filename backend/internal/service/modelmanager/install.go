package modelmanager

import (
	"context"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/track"
)

// LicenseRef names the license a user accepted: the ID and the version the screen showed.
type LicenseRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// AcceptLicense records that the user accepted the license of the model (ID and version as in the catalog). The
// install of a model that requires consent refuses without it (ErrConsentRequired): the API enforces it, not only
// the screen. A new version of a license asks for a new acceptance.
func (s *Service) AcceptLicense(modelID string, ref LicenseRef) error {
	m, ok := s.cat.Model(modelID)
	if !ok {
		return ErrNotFound
	}
	if ref.ID != m.License.ID || ref.Version != m.License.Version {
		return invalid("同意するライセンスが違います（%s %s が必要です）", m.License.ID, m.License.Version)
	}
	return s.state.Update(func(st *store.State) {
		if st.Consents == nil {
			st.Consents = map[string]store.Consent{}
		}
		st.Consents[m.License.ID] = store.Consent{Version: m.License.Version, AcceptedAt: s.Now().UTC()}
	})
}

// accepted reports whether the license of m was accepted in its current version.
func (s *Service) accepted(m catalog.Model) bool {
	c, ok := s.state.Get().Consents[m.License.ID]
	return ok && c.Version == m.License.Version
}

// InstallModel downloads and installs a model of the catalog, as a job (JobStatus; resumable: an interrupted
// download continues from its .part file, and every file is checked against its pinned SHA-256). It refuses at
// once, before anything is downloaded: without the engine, when the license needs an acceptance that was not given
// (accept records one, with the ID and version the screen showed), when the disk lacks the room
// (*fetch.NoSpaceError), and when the model file setting is pinned. The models of the install appear on the
// managed endpoint when the job succeeds.
func (s *Service) InstallModel(ctx context.Context, modelID string, accept *LicenseRef) (JobRef, error) {
	m, ok := s.cat.Model(modelID)
	if !ok {
		return JobRef{}, ErrNotFound
	}
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
	if m.ConsentRequired {
		if accept != nil {
			if err := s.AcceptLicense(modelID, *accept); err != nil {
				return JobRef{}, err
			}
		}
		if !s.accepted(m) {
			return JobRef{}, ErrConsentRequired
		}
	}
	if m.RequiresHFToken {
		if has, _ := s.Keys.Has(HuggingFaceProvider); !has {
			return JobRef{}, invalid("このモデルの取得には Hugging Face のトークンが要ります。先に設定してください")
		}
	}
	if v := s.view(m, s.state.Get()); v.State == ModelInstalled {
		return JobRef{}, ErrAlreadyInstalled
	}
	if err := s.Layout.CheckWritable(); err != nil {
		return JobRef{}, err
	}
	if err := s.checkModelSpace(m); err != nil {
		return JobRef{}, userFacing(ctx, err)
	}
	return s.enqueue(ctx, "model:"+m.ID, installPayload{Target: "model:" + m.ID})
}

// checkModelSpace is the refusal of an install that does not fit: what is not downloaded yet against the free space.
func (s *Service) checkModelSpace(m catalog.Model) error {
	var need int64
	for _, f := range m.Files {
		dest := s.Layout.ModelPath(f.Path())
		if !fileIsThere(dest, f.Size) {
			need += f.Size - partSize(dest, f.Size)
		}
	}
	if have := s.free(s.Layout.DataDir()); need > 0 && uint64(need) > have {
		return &fetch.NoSpaceError{Need: uint64(need), Free: have, Path: s.Layout.DataDir()}
	}
	return nil
}

// huggingFaceToken is the token for the downloads ("" when none is stored).
func (s *Service) huggingFaceToken() string {
	tok, err := s.Keys.Get(HuggingFaceProvider)
	if err != nil {
		return ""
	}
	return tok
}

// runModelInstall downloads the files of the model one after the other, then records the model and registers its
// workflows. Progress is by bytes over the files that are not in place.
func (s *Service) runModelInstall(ctx context.Context, id string, set func(track.Update)) error {
	m, ok := s.cat.Model(id)
	if !ok {
		return fmt.Errorf("model %q is not in the catalog", id)
	}
	if err := s.Layout.CheckWritable(); err != nil {
		return err
	}
	if err := s.checkModelSpace(m); err != nil {
		return err
	}
	var total, base int64
	for _, f := range m.Files {
		total += f.Size
	}
	opts := s.Fetch
	if opts.FreeBytes == nil {
		opts.FreeBytes = s.FreeBytes
	}
	opts.Token = s.huggingFaceToken
	installed := make([]store.InstalledFile, 0, len(m.Files))
	for i, f := range m.Files {
		dest := s.Layout.ModelPath(f.Path())
		label := fmt.Sprintf("%s（%d/%d）", f.Name, i+1, len(m.Files))
		opts.Progress = func(done, _ int64) {
			set(track.Update{
				Percent: int(float64(base+done) / float64(total) * 99), Phase: "download", Message: label,
				DoneBytes: base + done, TotalBytes: total,
			})
		}
		set(track.Update{Percent: int(float64(base) / float64(total) * 99), Phase: "download", Message: label, DoneBytes: base, TotalBytes: total})
		if err := s.fetchFile(ctx, dest, f, opts); err != nil {
			return err
		}
		installed = append(installed, store.InstalledFile{Path: f.Path(), SHA256: f.SHA256, Size: f.Size})
		base += f.Size
	}
	set(track.Update{Percent: 99, Phase: "register", Message: "モデルを登録しています", DoneBytes: total, TotalBytes: total})
	if err := s.registerWorkflows(ctx, m); err != nil {
		return err
	}
	names := make([]string, 0, len(m.Workflows))
	for _, w := range m.Workflows {
		names = append(names, w.Name)
	}
	if err := s.state.Update(func(st *store.State) {
		if st.Models == nil {
			st.Models = map[string]store.InstalledModel{}
		}
		st.Models[id] = store.InstalledModel{InstalledAt: s.Now().UTC(), Files: installed, Workflows: names, LicenseID: m.License.ID}
	}); err != nil {
		return err
	}
	set(track.Update{Percent: 100, Phase: "done", Message: "導入しました", DoneBytes: total, TotalBytes: total})
	return nil
}

// fetchFile makes dest the pinned file. A file that another installed model recorded with the same SHA-256 and
// that has the right size is trusted without hashing it again (a shared text encoder is several GB).
func (s *Service) fetchFile(ctx context.Context, dest string, f catalog.File, opts fetch.Options) error {
	if s.recordedGood(f) && fileIsThere(dest, f.Size) {
		if opts.Progress != nil {
			opts.Progress(f.Size, f.Size)
		}
		return nil
	}
	err := fetch.Download(ctx, dest, fetch.Source{URL: f.URL, SHA256: f.SHA256, Size: f.Size}, opts)
	if err == nil || ctx.Err() != nil {
		return err
	}
	return fmt.Errorf("%s: %w", f.Name, err)
}

// recordedGood reports whether an installed model recorded the file as verified.
func (s *Service) recordedGood(f catalog.File) bool {
	for _, im := range s.state.Get().Models {
		for _, r := range im.Files {
			if r.Path == f.Path() && r.SHA256 == f.SHA256 && r.Size == f.Size {
				return true
			}
		}
	}
	return false
}

// SetHuggingFaceToken stores the optional Hugging Face token (credential provider "huggingface"). No model of the
// catalog needs it; it is sent as a bearer token to huggingface.co only, never to the CDN a download is redirected
// to. Empty or malformed is *InvalidError.
func (s *Service) SetHuggingFaceToken(token string) error {
	token, problem := credential.CleanAPIKey(token)
	if problem != "" {
		return invalid("%s", problem)
	}
	if err := s.Keys.Set(HuggingFaceProvider, token); errors.Is(err, credential.ErrTooLarge) {
		return invalid("トークンが長すぎます")
	} else if err != nil {
		return err
	}
	return nil
}

// ClearHuggingFaceToken removes the token; removing a missing one is not an error.
func (s *Service) ClearHuggingFaceToken() error {
	if err := s.Keys.Delete(HuggingFaceProvider); err != nil && !errors.Is(err, credential.ErrNotFound) {
		return err
	}
	return nil
}

// HuggingFaceTokenStatus says whether a token is stored and its last characters; it never returns the token.
func (s *Service) HuggingFaceTokenStatus() (configured bool, lastChars string, err error) {
	ok, err := s.Keys.Has(HuggingFaceProvider)
	if err != nil || !ok {
		return false, "", err
	}
	hint, err := credential.Hint(s.Keys, HuggingFaceProvider)
	return true, hint, err
}
