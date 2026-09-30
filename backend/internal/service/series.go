package service

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// SeriesService keeps the canon of a Series and starts Episodes from it (F-29).
type SeriesService struct {
	*core
	series *repository.SeriesRepository
}

// SeriesView is the Series page: the canon, its history and the Episodes.
type SeriesView struct {
	Series   domain.Series
	Canon    domain.SeriesCanon
	Versions []domain.SeriesVersion
	Episodes []domain.Project
}

// View returns the Series, its current canon, the history of the canon (newest first) and its
// Episodes. An unknown Series is ErrNotFound.
func (s *SeriesService) View(ctx context.Context, seriesID string) (SeriesView, error) {
	series, err := s.series.Get(ctx, seriesID)
	if err != nil {
		return SeriesView{}, notFound(err)
	}
	canon, err := s.series.Canon(ctx, seriesID)
	if err != nil {
		return SeriesView{}, notFound(err)
	}
	versions, err := s.series.Versions(ctx, seriesID)
	if err != nil {
		return SeriesView{}, err
	}
	episodes, err := s.series.Episodes(ctx, seriesID)
	if err != nil {
		return SeriesView{}, err
	}
	return SeriesView{Series: series, Canon: canon, Versions: versions, Episodes: episodes}, nil
}

// Create makes a Series (POST /series). With fromProjectID the Character, Product and Location of
// that Project are registered as the canon and the Project becomes Episode 1; an empty name then
// defaults to the name of the Project. A name that is empty (and no Project to take it from) or
// too long, or a Project that already belongs to a Series, is a ValidationError; an unknown
// Project is ErrNotFound.
func (s *SeriesService) Create(ctx context.Context, name, fromProjectID string) (domain.Series, error) {
	fromProjectID = strings.TrimSpace(fromProjectID)
	if strings.TrimSpace(name) == "" && fromProjectID != "" {
		p, err := s.getProject(ctx, fromProjectID)
		if err != nil {
			return domain.Series{}, err
		}
		name = p.Name
	}
	name, ok := domain.ValidateSeriesName(name)
	if !ok {
		return domain.Series{}, invalid("name", "Series 名は 1〜%d 文字で入力してください", domain.MaxSeriesNameRunes)
	}
	series, err := s.series.Create(ctx, name, fromProjectID)
	switch {
	case errors.Is(err, repository.ErrConflict):
		return domain.Series{}, invalid("from_project_id", "この Project はすでに Series に属しています")
	case err != nil:
		return domain.Series{}, notFound(err)
	}
	return series, nil
}

// CreateEpisode starts the next Episode of the Series from its canon (POST /series/:id/episodes)
// and returns the new Project. previousEpisodeID, when set, must be an Episode of the same Series
// (else a ValidationError): the new Episode then starts from that Episode's final World State.
func (s *SeriesService) CreateEpisode(ctx context.Context, seriesID, previousEpisodeID string) (domain.Project, error) {
	project := domain.Project{
		ID: domain.NewID(), State: domain.StateDraft, Mode: domain.ModeAuto,
		ConfirmationLevel:  domain.ConfirmKeyCheck,
		ProductionTemplate: domain.TemplateProductCM,
		QualityPolicy:      domain.QualityBalanced,
	}
	_, err := s.series.CreateEpisode(ctx, seriesID, strings.TrimSpace(previousEpisodeID), project)
	switch {
	case errors.Is(err, repository.ErrNotInSeries):
		return domain.Project{}, invalid("previous_episode_id", "前の話はこの Series の Episode を指定してください")
	case err != nil:
		return domain.Project{}, notFound(err)
	}
	return s.getProject(ctx, project.ID)
}

// UpdateCharacter sets the given fields (domain.SeriesCharacterFields) of a canonical Character and
// returns the new view (PUT /series/:id/characters/:characterId). The canon gets a new version;
// Episodes that already copied the Character are not touched. An unknown field or a value that is
// too long is a ValidationError; an unknown Series or Character is ErrNotFound.
func (s *SeriesService) UpdateCharacter(ctx context.Context, seriesID, characterID string, fields map[string]string) (SeriesView, error) {
	if len(fields) == 0 {
		return SeriesView{}, invalid("", "更新する項目がありません")
	}
	clean := make(map[string]string, len(fields))
	for name, value := range fields {
		if !domain.IsSeriesCharacterField(name) {
			return SeriesView{}, invalid(name, "この項目は編集できません")
		}
		value = strings.TrimSpace(value)
		if utf8.RuneCountInString(value) > domain.MaxWorldValueRunes {
			return SeriesView{}, invalid(name, "%d 文字以内で入力してください", domain.MaxWorldValueRunes)
		}
		clean[name] = value
	}
	if err := s.series.UpdateCharacter(ctx, seriesID, characterID, clean); err != nil {
		return SeriesView{}, notFound(err)
	}
	return s.View(ctx, seriesID)
}

// UpdateStyle sets the canonical Style text (PUT /series/:id/style) and returns the new view. New
// Episodes get it in their Brief; existing Episodes keep theirs. A Style over
// domain.MaxStyleRunes is a ValidationError.
func (s *SeriesService) UpdateStyle(ctx context.Context, seriesID, style string) (SeriesView, error) {
	style = strings.TrimSpace(style)
	if utf8.RuneCountInString(style) > domain.MaxStyleRunes {
		return SeriesView{}, invalid("style", "%d 文字以内で入力してください", domain.MaxStyleRunes)
	}
	if err := s.series.UpdateStyle(ctx, seriesID, style); err != nil {
		return SeriesView{}, notFound(err)
	}
	return s.View(ctx, seriesID)
}
