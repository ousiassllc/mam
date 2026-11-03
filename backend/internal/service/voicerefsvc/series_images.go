package voicerefsvc

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// EpisodeImages are the Library images (kind character) of one Episode of a Series: the images the canon's Character can take as its face or body.
type EpisodeImages struct {
	Project domain.Project
	Items   []domain.ReferenceItem
}

// SeriesImages lists, for each Episode of the Series that has any, its Library images of kind character (a Series has no Library of its own: the
// images of a canonical Character are chosen from those of its Episodes, as CreateForSeries takes them from the Library of a Project). An
// unknown Series is ErrNotFound.
func (s *Service) SeriesImages(ctx context.Context, seriesID string) ([]EpisodeImages, error) {
	if err := s.exists(ctx, owner{id: seriesID, series: true}); err != nil {
		return nil, err
	}
	episodes, err := s.series.Episodes(ctx, seriesID)
	if err != nil {
		return nil, err
	}
	var out []EpisodeImages
	for _, p := range episodes {
		items, err := s.refs.List(ctx, p.ID, repository.ReferenceFilter{Kind: domain.RefCharacter})
		if err != nil {
			return nil, err
		}
		if len(items) > 0 {
			out = append(out, EpisodeImages{Project: p, Items: items})
		}
	}
	return out, nil
}
