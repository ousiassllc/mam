package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// ReferenceService runs the Reference Library of a Project (F-11, spec/spec.md §10.1): registering,
// finding and removing reference images, and adding them to the Reference Pack of a Shot.
type ReferenceService struct {
	*core
}

// LibraryQuery narrows the Library listing. Kind is "" or a library kind.
type LibraryQuery struct {
	Q    string
	Kind string
	Tag  string
}

// LibraryView is the reference_library_page.
type LibraryView struct {
	Project domain.Project
	Query   LibraryQuery
	Items   []domain.ReferenceItem
}

// Library returns the Library of the Project filtered by q. An unknown kind is a ValidationError.
func (s *ReferenceService) Library(ctx context.Context, projectID string, q LibraryQuery) (LibraryView, error) {
	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return LibraryView{}, err
	}
	q = LibraryQuery{Q: strings.TrimSpace(q.Q), Kind: strings.TrimSpace(q.Kind), Tag: strings.TrimSpace(q.Tag)}
	f := repository.ReferenceFilter{Query: q.Q, Tag: q.Tag}
	if q.Kind != "" {
		f.Kind = domain.ReferenceKind(q.Kind)
		if !domain.ValidLibraryKind(f.Kind) {
			return LibraryView{}, invalid("kind", "種別が正しくありません")
		}
	}
	items, err := s.references.List(ctx, projectID, f)
	if err != nil {
		return LibraryView{}, err
	}
	return LibraryView{Project: project, Query: q, Items: items}, nil
}

// UploadReferenceInput is the form of a reference upload. Tags is the raw text of the tag field.
type UploadReferenceInput struct {
	File Upload
	Kind string
	Name string
	Tags string
}

// Upload registers an image of the user in the Library (source uploaded) and returns the whole
// Library. Nothing is stored when any input is invalid.
func (s *ReferenceService) Upload(ctx context.Context, projectID string, in UploadReferenceInput) (LibraryView, error) {
	if _, err := s.getProject(ctx, projectID); err != nil {
		return LibraryView{}, err
	}
	kind := domain.ReferenceKind(strings.TrimSpace(in.Kind))
	if !domain.ValidLibraryKind(kind) {
		return LibraryView{}, invalid("kind", "種別を選んでください")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return LibraryView{}, invalid("name", "名前を入力してください")
	}
	if utf8.RuneCountInString(name) > domain.MaxReferenceNameRunes {
		return LibraryView{}, invalid("name", "名前は %d 文字以内で入力してください", domain.MaxReferenceNameRunes)
	}
	tags, err := domain.ParseTags(in.Tags)
	if err != nil {
		return LibraryView{}, invalid("tags", "タグは各 %d 文字以内・%d 個までにしてください", domain.MaxReferenceTagRunes, domain.MaxReferenceTags)
	}
	if in.File.Open == nil {
		return LibraryView{}, invalid("file", "画像を選んでください")
	}
	if s.assetsDir == "" {
		return LibraryView{}, errors.New("uploads are not configured: assets directory is empty")
	}
	if k, ok := extensions[strings.ToLower(filepath.Ext(in.File.Name))]; ok && k.assetType != domain.AssetImage {
		return LibraryView{}, invalid("file", "%s", "画像（jpg/png/webp/gif）を選んでください")
	}
	stored, err := s.storeUpload(projectID, in.File)
	if err != nil {
		return LibraryView{}, retag(err, "file")
	}
	stored.asset.Type = domain.AssetReference
	item := domain.ReferenceItem{
		ID: domain.NewID(), ProjectID: projectID, AssetID: stored.asset.ID, Kind: kind,
		Name: name, Tags: tags, Source: domain.SourceUploaded,
	}
	if err := s.references.Add(ctx, item, stored.asset); err != nil {
		_ = os.Remove(stored.path)
		return LibraryView{}, notFound(err)
	}
	return s.Library(ctx, projectID, LibraryQuery{})
}

// retag moves a ValidationError of the shared upload code to the form field of the caller.
func retag(err error, field string) error {
	var v *ValidationError
	if errors.As(err, &v) {
		return &ValidationError{Field: field, Message: v.Message}
	}
	return err
}

// Remove takes the entry out of the Library (the image and the references Shots hold stay) and
// returns the Library under q, the filter of the list the entry was removed from.
func (s *ReferenceService) Remove(ctx context.Context, projectID, referenceID string, q LibraryQuery) (LibraryView, error) {
	if _, err := s.getProject(ctx, projectID); err != nil {
		return LibraryView{}, err
	}
	if err := s.references.Delete(ctx, projectID, referenceID); err != nil {
		return LibraryView{}, notFound(err)
	}
	return s.Library(ctx, projectID, q)
}

// Pin adds the Library image to the Reference Pack of the Shot and returns the Shot Inspector. An
// empty referenceID is a ValidationError; an entry of another Project is ErrNotFound; a Shot being
// generated is ErrBusy.
func (s *ReferenceService) Pin(ctx context.Context, shotID, referenceID string) (Inspector, error) {
	referenceID = strings.TrimSpace(referenceID)
	if referenceID == "" {
		return Inspector{}, invalid("reference_id", "参照画像を選んでください")
	}
	err := s.references.Pin(ctx, shotID, referenceID)
	switch {
	case errors.Is(err, repository.ErrShotBusy):
		return Inspector{}, ErrBusy
	case err != nil:
		return Inspector{}, notFound(err)
	}
	return s.director.Inspector(ctx, shotID)
}
