// Package series holds the MCP tools of the Series screen (/series/:id): the canon and its history, the
// Episodes, creating a Series and an Episode, and editing the canon (a Character, the Style). Series
// notes are in the notes package.
package series

import (
	"context"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Register adds the tools of this package.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	s := svc.Series
	mcpkit.Read(r, "get_series",
		"Series の正本（Style・登場人物・商品・場所）、正本のバージョン履歴、Episode 一覧、Episode に使える Brand Template を返す。series_id は list_projects の series_id / The canon, its history and the Episodes of a Series.",
		get(s))
	mcpkit.Write(r, "create_series",
		"Series を作る。from_project_id を渡すとその Project の登場人物・商品・場所を正本にして第 1 話にする（name を省略するとその Project の名前）/ Make a Series, optionally from an existing Project (it becomes Episode 1).",
		create(s))
	mcpkit.Write(r, "create_episode",
		"Series の次の Episode（新しい Project）を、正本から作る。previous_episode_id を渡すとその話の最終 World State から始める / Start the next Episode of a Series from its canon.",
		createEpisode(s))
	mcpkit.Write(r, "update_series_character",
		"Series の正本の登場人物の属性（髪型・衣装・声・性格）を、渡した項目だけ更新する。正本に新しいバージョンができる。作成済みの Episode は変わらない / Update the canonical attributes of a Character of a Series.",
		updateCharacter(s))
	mcpkit.Write(r, "update_series_style",
		"Series の正本の Style（500 文字以内）を更新する。新しい Episode の Brief に入る。作成済みの Episode は変わらない / Set the canonical Style of a Series.",
		updateStyle(s))
}

// GetInput names the Series.
type GetInput struct {
	SeriesID string `json:"series_id"`
}

// Episode is a Project of the Series.
type Episode struct {
	ProjectID     string `json:"project_id"`
	Name          string `json:"name"`
	State         string `json:"state"`
	EpisodeNumber int    `json:"episode_number"`
}

// Template is a Brand Template a new Episode can start from.
type Template struct {
	ID   string `json:"id" jsonschema:"create_episode の brand_template_id"`
	Name string `json:"name"`
}

// Version is one entry of the history of the canon.
type Version struct {
	Number    int       `json:"version_number"`
	CreatedAt time.Time `json:"created_at"`
}

// Canon is the current canon of the Series.
type Canon struct {
	Style      string                  `json:"style,omitempty"`
	StyleAsset string                  `json:"style_reference_asset_id,omitempty" jsonschema:"Style の参照画像のアセット ID"`
	Characters []domain.WorldCharacter `json:"characters"`
	Products   []domain.WorldProduct   `json:"products"`
	Locations  []domain.WorldLocation  `json:"locations"`
}

// View is the Series page.
type View struct {
	SeriesID  string     `json:"series_id"`
	Name      string     `json:"name"`
	Version   int        `json:"version" jsonschema:"正本の最新バージョン番号"`
	CreatedAt time.Time  `json:"created_at"`
	Canon     Canon      `json:"canon"`
	Versions  []Version  `json:"versions" jsonschema:"正本のバージョン履歴（新しい順）"`
	Episodes  []Episode  `json:"episodes" jsonschema:"話数順"`
	Templates []Template `json:"brand_templates"`
}

func viewOf(v service.SeriesView) View {
	out := View{
		SeriesID: v.Series.ID, Name: v.Series.Name, Version: v.Series.Version, CreatedAt: v.Series.CreatedAt,
		Canon: Canon{
			Style: v.Canon.Style, StyleAsset: v.Canon.StyleReferenceAssetID,
			Characters: orEmpty(v.Canon.Characters), Products: orEmpty(v.Canon.Products), Locations: orEmpty(v.Canon.Locations),
		},
		Versions:  make([]Version, 0, len(v.Versions)),
		Episodes:  make([]Episode, 0, len(v.Episodes)),
		Templates: make([]Template, 0, len(v.Templates)),
	}
	for _, x := range v.Versions {
		out.Versions = append(out.Versions, Version{Number: x.Number, CreatedAt: x.CreatedAt})
	}
	for _, p := range v.Episodes {
		out.Episodes = append(out.Episodes, Episode{ProjectID: p.ID, Name: p.Name, State: string(p.State), EpisodeNumber: p.EpisodeNumber})
	}
	for _, t := range v.Templates {
		out.Templates = append(out.Templates, Template{ID: t.ID, Name: t.Name})
	}
	return out
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func get(svc *service.SeriesService) mcpkit.Handler[GetInput, View] {
	return func(ctx context.Context, in GetInput) (View, error) {
		v, err := svc.View(ctx, in.SeriesID)
		if err != nil {
			return View{}, err
		}
		return viewOf(v), nil
	}
}

// CreateInput is the form of a new Series.
type CreateInput struct {
	Name          string `json:"name,omitempty" jsonschema:"Series 名（1〜100 文字）。from_project_id があれば省略できる"`
	FromProjectID string `json:"from_project_id,omitempty" jsonschema:"正本の元にする Project（まだ Series に属していないもの）。第 1 話になる"`
}

// CreateOutput is the new Series.
type CreateOutput struct {
	SeriesID string `json:"series_id"`
	Name     string `json:"name"`
}

func create(svc *service.SeriesService) mcpkit.Handler[CreateInput, CreateOutput] {
	return func(ctx context.Context, in CreateInput) (CreateOutput, error) {
		s, err := svc.Create(ctx, in.Name, in.FromProjectID)
		if err != nil {
			return CreateOutput{}, err
		}
		return CreateOutput{SeriesID: s.ID, Name: s.Name}, nil
	}
}

// CreateEpisodeInput names the Series and what the Episode starts from.
type CreateEpisodeInput struct {
	SeriesID          string `json:"series_id"`
	PreviousEpisodeID string `json:"previous_episode_id,omitempty" jsonschema:"同じ Series の Episode（Project）の ID。その最終 World State から始める"`
	BrandTemplateID   string `json:"brand_template_id,omitempty" jsonschema:"get_series の brand_templates の ID。制約を Episode にコピーする"`
}

// CreateEpisodeOutput is the new Episode.
type CreateEpisodeOutput struct {
	ProjectID     string `json:"project_id"`
	Name          string `json:"name"`
	EpisodeNumber int    `json:"episode_number"`
}

func createEpisode(svc *service.SeriesService) mcpkit.Handler[CreateEpisodeInput, CreateEpisodeOutput] {
	return func(ctx context.Context, in CreateEpisodeInput) (CreateEpisodeOutput, error) {
		p, err := svc.CreateEpisode(ctx, in.SeriesID, in.PreviousEpisodeID, in.BrandTemplateID)
		if err != nil {
			return CreateEpisodeOutput{}, err
		}
		return CreateEpisodeOutput{ProjectID: p.ID, Name: p.Name, EpisodeNumber: p.EpisodeNumber}, nil
	}
}

// UpdateCharacterInput names the canonical Character and the attributes to set; one left out stays as it is.
type UpdateCharacterInput struct {
	SeriesID    string  `json:"series_id"`
	CharacterID string  `json:"character_id" jsonschema:"get_series の canon.characters の id"`
	Hairstyle   *string `json:"hairstyle,omitempty"`
	Wardrobe    *string `json:"wardrobe,omitempty"`
	Voice       *string `json:"voice,omitempty"`
	Personality *string `json:"personality,omitempty"`
}

func updateCharacter(svc *service.SeriesService) mcpkit.Handler[UpdateCharacterInput, View] {
	return func(ctx context.Context, in UpdateCharacterInput) (View, error) {
		fields := map[string]string{}
		for name, v := range map[string]*string{"hairstyle": in.Hairstyle, "wardrobe": in.Wardrobe, "voice": in.Voice, "personality": in.Personality} {
			if v != nil {
				fields[name] = *v
			}
		}
		v, err := svc.UpdateCharacter(ctx, in.SeriesID, in.CharacterID, fields)
		if err != nil {
			return View{}, err
		}
		return viewOf(v), nil
	}
}

// UpdateStyleInput names the Series and its new Style.
type UpdateStyleInput struct {
	SeriesID string `json:"series_id"`
	Style    string `json:"style" jsonschema:"正本の Style（500 文字以内。空にもできる）"`
}

func updateStyle(svc *service.SeriesService) mcpkit.Handler[UpdateStyleInput, View] {
	return func(ctx context.Context, in UpdateStyleInput) (View, error) {
		v, err := svc.UpdateStyle(ctx, in.SeriesID, in.Style)
		if err != nil {
			return View{}, err
		}
		return viewOf(v), nil
	}
}
