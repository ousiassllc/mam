package modelmanager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hfplan"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/track"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
)

// errHFAccess is why a download of a gated file was refused after the install was queued.
var errHFAccess = errors.New("modelmanager: Hugging Face refused the download")

// UserModelView is a file the user added from Hugging Face (hfplan.UserModelView).
type UserModelView = hfplan.UserModelView

// runHFInstall downloads the file against its pinned size and SHA-256 (fetch.Download: resumable, space checked, a mismatch
// leaves no file), then records it as a user model and, for an all-in-one SDXL checkpoint, registers the two workflows of
// the SDXL templates with its file name.
func (s *Service) runHFInstall(ctx context.Context, p hfplan.Payload, set func(track.Update)) error {
	if p.NFAA && !s.AdultContentEnabled() { // the gate was closed after the job was queued (or before a restart resumed it)
		return ErrAdultNotConfirmed
	}
	dest := s.Layout.ModelPath(p.Rel())
	if err := errors.Join(s.Layout.CheckWritable(), s.hfError(hfplan.CheckDest(dest, p, s.recordedAt))); err != nil {
		return err
	}
	opts := s.Fetch
	if opts.FreeBytes == nil {
		opts.FreeBytes = s.FreeBytes
	}
	opts.Token = s.huggingFaceToken
	opts.Progress = func(done, total int64) {
		set(track.Update{Percent: int(float64(done) / float64(total) * 99), Phase: "download", Message: p.Name, DoneBytes: done, TotalBytes: total})
	}
	set(track.Update{Phase: "download", Message: p.Name, TotalBytes: p.Size})
	err := s.fetchFile(ctx, dest, catalog.File{Name: p.Name, Folder: p.Folder, URL: p.URL, SHA256: p.SHA256, Size: p.Size}, opts)
	var he *fetch.HTTPError
	if errors.As(err, &he) && (he.Status == 401 || he.Status == 403) && ctx.Err() == nil {
		return &userError{fmt.Sprintf("Hugging Face が取得を断りました（%d）。gated のリポジトリは、トークンの設定と、%s でのアクセスの承認が要ります", he.Status, s.hf.PageURL(p.Repo)), errHFAccess}
	}
	if err != nil {
		return err
	}
	set(track.Update{Percent: 99, Phase: "register", Message: "モデルを登録しています", DoneBytes: p.Size, TotalBytes: p.Size})
	var family string
	var workflows []string
	if p.Folder == "checkpoints" {
		if family = hfplan.FamilyOfFile(dest); family == hfplan.FamilySDXL {
			if workflows, err = hfplan.RegisterCheckpoint(ctx, s.writeModel, p); err != nil {
				return err
			}
		}
	}
	if err := s.state.Update(func(st *store.State) {
		if st.UserModels == nil {
			st.UserModels = map[string]store.UserModel{}
		}
		st.UserModels[p.Rel()] = hfplan.Record(p, family, workflows, s.Now())
	}); err != nil {
		return err
	}
	set(track.Update{Percent: 100, Phase: "done", Message: "導入しました", DoneBytes: p.Size, TotalBytes: p.Size})
	return nil
}

// writeModel creates a model of the bundled endpoint, or replaces the one of that name (hfplan.ModelWriter).
func (s *Service) writeModel(ctx context.Context, name string, m localcfg.Model) error {
	if err := s.ensureEndpoint(ctx); err != nil {
		return err
	}
	in := localmodels.ModelInput{Endpoint: BundledEndpoint, Name: name, Model: m}
	err := s.Models.CreateModel(ctx, in)
	if errors.Is(err, localmodels.ErrExists) {
		err = s.Models.UpdateModel(ctx, BundledEndpoint, name, in)
	}
	return err
}

// UserModels lists the files added from Hugging Face, newest first. A file of a repository tagged not-for-all-audiences is
// left out while the age gate is closed (DeleteUserModel still removes it).
func (s *Service) UserModels() []UserModelView {
	st := s.state.Get()
	return hfplan.UserModels(st, s.AdultContentEnabled(), BundledEndpoint,
		func(um store.UserModel) bool { return fileIsThere(s.Layout.ModelPath(um.Path), um.Size) },
		func(path string) []string { return s.catalogHolders(st, path) })
}

// catalogHolders lists the installed catalog models that hold the file at path.
func (s *Service) catalogHolders(st store.State, path string) (ids []string) {
	for id, im := range st.Models {
		if slices.ContainsFunc(im.Files, func(f store.InstalledFile) bool { return f.Path == path }) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// DeleteUserModel removes a file added from Hugging Face, by its path (models/<folder>/<name>): the models created for it
// and the file. The file stays while an installed catalog model holds the same file (the record goes all the same). A model
// that an image purpose or a Quality Policy chooses is refused (*InvalidError), a running install of it is ErrBusy, an
// unknown path is ErrNotInstalled. It works with the age gate closed: clearing files is always possible.
func (s *Service) DeleteUserModel(ctx context.Context, path string) error {
	st := s.state.Get()
	um, ok := st.UserModels[path]
	switch {
	case !ok:
		return ErrNotInstalled
	case s.activeJob(hfplan.Target(um.Repo, um.Commit, um.RepoPath)) != "":
		return ErrBusy
	}
	if err := s.checkNotInUse(um.Workflows); err != nil {
		return err
	}
	for _, name := range um.Workflows {
		if err := s.Models.DeleteModel(ctx, BundledEndpoint, name); err != nil && !errors.Is(err, localmodels.ErrNotFound) {
			return err
		}
	}
	var failed error
	if dest := s.Layout.ModelPath(um.Path); len(s.catalogHolders(st, um.Path)) == 0 {
		for _, p := range []string{dest, fetch.PartPath(dest)} {
			if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
				failed = errors.Join(failed, err)
			}
		}
	}
	if err := s.state.Update(func(st *store.State) { delete(st.UserModels, path) }); err != nil {
		return err
	}
	return failed
}
