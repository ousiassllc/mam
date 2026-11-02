// Package history is the history of the generation test: the list of the results, a result with the inputs of its batch and where it
// was imported to, the files of the history, and the prompt history. Package gentest embeds a Store, so the service answers these calls.
package history

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/projectlink"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
)

// Store reads and edits the history: the generation test's tables and its directory of files.
type Store struct {
	repo *gentestrepo.Repository
	// dir is the directory of the history's files (<data-dir>/gentest).
	dir string
}

// New returns the Store of the tables repo and the directory dir.
func New(repo *gentestrepo.Repository, dir string) *Store { return &Store{repo: repo, dir: dir} }

// Filter narrows the history list. Limit 0 is 100.
type Filter struct {
	Model string
	// Models keeps the results of any of these models.
	Models       []string
	Media        string
	FavoriteOnly bool
	Limit        int
	Offset       int
}

// History lists the results, newest first.
func (s *Store) History(ctx context.Context, f Filter) ([]record.ResultView, error) {
	if f.Media != "" && f.Media != gentestrepo.MediaImage && f.Media != gentestrepo.MediaVideo {
		return nil, gterr.Invalid("種類は image か video にしてください")
	}
	if f.Limit <= 0 {
		f.Limit = 100
	}
	list, err := s.repo.ListResults(ctx, gentestrepo.Filter{Model: f.Model, Models: f.Models, Media: f.Media, FavoriteOnly: f.FavoriteOnly, Limit: f.Limit, Offset: f.Offset})
	return record.ViewsOf(list), err
}

// HistoryModels lists the models the history has results of (the choices of the model filter).
func (s *Store) HistoryModels(ctx context.Context) ([]string, error) { return s.repo.Models(ctx) }

// Pickable lists the succeeded image results, newest first: the images a new batch can take from the history.
func (s *Store) Pickable(ctx context.Context, limit int) ([]record.ResultView, error) {
	list, err := s.repo.ListResults(ctx, gentestrepo.Filter{Media: gentestrepo.MediaImage, SucceededOnly: true, Limit: max(limit, 1)})
	return record.ViewsOf(list), err
}

// Spend is the one-line total of the history.
func (s *Store) Spend(ctx context.Context) (record.Spend, error) {
	sp, err := s.repo.Spend(ctx)
	return record.Spend{Items: sp.Items, PricedUSD: sp.PricedUSD, UnknownItems: sp.UnknownItems}, err
}

// Result returns a result with the inputs of its batch and where it was imported to (gterr.ErrNotFound).
func (s *Store) Result(ctx context.Context, id string) (record.ResultDetail, error) {
	r, err := s.repo.GetResult(ctx, id)
	if err != nil {
		return record.ResultDetail{}, gterr.NotFound(err)
	}
	batch, err := s.repo.GetBatch(ctx, r.BatchID)
	if err != nil {
		return record.ResultDetail{}, gterr.NotFound(err)
	}
	inputs, err := s.repo.Inputs(ctx, r.BatchID)
	if err != nil {
		return record.ResultDetail{}, err
	}
	imports, err := s.repo.Imports(ctx, id)
	if err != nil {
		return record.ResultDetail{}, err
	}
	d := record.ResultDetail{ResultView: record.ViewOf(r), Inputs: []record.InputView{}, Imports: []record.ImportView{}, BatchEstimatedUSD: batch.EstimatedCost}
	for _, in := range inputs {
		d.Inputs = append(d.Inputs, record.InputView{ID: in.ID, Role: in.Role, MimeType: in.MimeType, SizeBytes: in.SizeBytes, Strength: in.Strength, Origin: decode[record.InputOrigin](in.Origin)})
	}
	for _, im := range imports {
		d.Imports = append(d.Imports, record.ImportView{ID: im.ID, ProjectID: im.ProjectID, AssetID: im.AssetID, ReferenceItemID: im.ReferenceItemID, Target: decode[projectlink.ImportTarget](im.Target), CreatedAt: im.CreatedAt})
	}
	return d, nil
}

// File is a stored file for the HTTP handlers: the path, the type and a name for a download.
type File struct {
	Path string
	Mime string
	Name string
}

// ResultFile is the file of a succeeded result (gterr.ErrNotFound for any other).
func (s *Store) ResultFile(ctx context.Context, id string) (File, error) {
	r, err := s.repo.GetResult(ctx, id)
	if err != nil {
		return File{}, gterr.NotFound(err)
	}
	if r.Status != gentestrepo.StatusSucceeded || r.FilePath == "" {
		return File{}, gterr.ErrNotFound
	}
	return File{Path: s.filePath(r.FilePath), Mime: r.MimeType, Name: r.ID + filepath.Ext(r.FilePath)}, nil
}

// InputFile is the file of an input image of a batch (gterr.ErrNotFound).
func (s *Store) InputFile(ctx context.Context, id string) (File, error) {
	in, err := s.repo.GetInput(ctx, id)
	if err != nil {
		return File{}, gterr.NotFound(err)
	}
	return File{Path: s.filePath(in.FilePath), Mime: in.MimeType, Name: in.ID + filepath.Ext(in.FilePath)}, nil
}

// filePath is the path of a file of the history; relative is the stored path below the gentest directory.
func (s *Store) filePath(relative string) string {
	return filepath.Join(s.dir, filepath.FromSlash(relative))
}

// SetFavorite sets or clears the favorite mark of a result (gterr.ErrNotFound).
func (s *Store) SetFavorite(ctx context.Context, id string, favorite bool) error {
	return gterr.NotFound(s.repo.SetFavorite(ctx, id, favorite))
}

// DeleteResult removes a result with its file; the last result of a batch takes the batch, its inputs and its directory with it.
// A result that has not finished is a *gterr.ConflictError (cancel the batch first).
func (s *Store) DeleteResult(ctx context.Context, id string) error {
	r, err := s.repo.GetResult(ctx, id)
	if err != nil {
		return gterr.NotFound(err)
	}
	if r.Status == gentestrepo.StatusQueued || r.Status == gentestrepo.StatusRunning {
		return gterr.Conflict("生成が終わっていない結果は削除できません。バッチをキャンセルして、終わってから削除してください")
	}
	batchID, left, err := s.repo.DeleteResult(ctx, id)
	if err != nil {
		return gterr.NotFound(err)
	}
	if left == 0 {
		return s.removeDir(batchID)
	}
	if r.FilePath != "" {
		if err := os.Remove(s.filePath(r.FilePath)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// DeleteBatch removes a batch with all its results, inputs and files. A batch that is not over is a *gterr.ConflictError.
func (s *Store) DeleteBatch(ctx context.Context, batchID string) error {
	if _, err := s.repo.GetBatch(ctx, batchID); err != nil {
		return gterr.NotFound(err)
	}
	counts, err := s.repo.Counts(ctx, batchID)
	if err != nil {
		return err
	}
	if counts.Queued+counts.Running > 0 {
		return gterr.Conflict("生成が終わっていないバッチは削除できません。キャンセルして、終わってから削除してください")
	}
	if err := s.repo.DeleteBatch(ctx, batchID); err != nil {
		return gterr.NotFound(err)
	}
	return s.removeDir(batchID)
}

func (s *Store) removeDir(batchID string) error {
	if batchID == "" || batchID != filepath.Base(batchID) {
		return nil
	}
	return os.RemoveAll(filepath.Join(s.dir, batchID))
}

// Prompts lists the prompt history, the most recently used first.
func (s *Store) Prompts(ctx context.Context) ([]record.PromptView, error) {
	list, err := s.repo.Prompts(ctx)
	out := make([]record.PromptView, len(list))
	for i, p := range list {
		out[i] = record.PromptView{ID: p.ID, Prompt: p.Prompt, NegativePrompt: p.NegativePrompt, UseCount: p.UseCount, LastUsedAt: p.LastUsedAt}
	}
	return out, err
}

// Prompt returns one entry of the prompt history (gterr.ErrNotFound).
func (s *Store) Prompt(ctx context.Context, id string) (record.PromptView, error) {
	p, err := s.repo.GetPrompt(ctx, id)
	if err != nil {
		return record.PromptView{}, gterr.NotFound(err)
	}
	return record.PromptView{ID: p.ID, Prompt: p.Prompt, NegativePrompt: p.NegativePrompt, UseCount: p.UseCount, LastUsedAt: p.LastUsedAt}, nil
}

// DeletePrompt removes a prompt from the history. The results that used it keep their own copy (gterr.ErrNotFound).
func (s *Store) DeletePrompt(ctx context.Context, id string) error {
	return gterr.NotFound(s.repo.DeletePrompt(ctx, id))
}

// decode reads a JSON column the service wrote itself; a column it cannot read is the zero value.
func decode[T any](raw []byte) T {
	var v T
	_ = json.Unmarshal(raw, &v)
	return v
}
