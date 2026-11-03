// Package character holds create_character: a Character made by hand, in a Project (its World State) or in the canon of a Series. The
// Project's planning makes the Characters when an LLM runs it; this is the way to have them without one (#1001), and to register a
// Character in the canon first and use it from the Episodes later. The screen's routes are POST /projects/:id/characters and
// POST /series/:id/characters.
package character

import (
	"context"
	"errors"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
)

// Register adds create_character.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	mcpkit.Write(r, "create_character",
		"登場人物を直接作る（LLM の企画を待たない）。project_id を渡すとその Project の World State に、series_id を渡すと Series の正本に作る（どちらか一方）。名前（必須・100 文字以内）、髪型・衣装・性格・声（各 200 文字以内）、face / body の参照画像（Reference Library の kind=character の画像）を作成時に渡せる。返る character_id は assign_character_reference の entity_id（Project）、update_series_character・update_character_voice_reference の character_id になる。同じ名前の登場人物がすでにあれば validation。Series の正本に作ると正本に新しいバージョンができ、作成済みの Episode は変わらない（次の Episode から）。LLM を使わない / Make a Character by hand in a Project or in the canon of a Series.",
		create(svc))
}

// Input names where the Character is made and what it is made of.
type Input struct {
	ProjectID   string `json:"project_id,omitempty" jsonschema:"Project の World State に作るとき（series_id とどちらか一方）"`
	SeriesID    string `json:"series_id,omitempty" jsonschema:"Series の正本に作るとき（project_id とどちらか一方）"`
	BranchID    string `json:"branch_id,omitempty" jsonschema:"project_id のとき、Branch の World State に作る Branch ID。省略すると本線"`
	Name        string `json:"name" jsonschema:"名前（1〜100 文字）。World State / 正本の中で重複できない（大文字小文字は区別しない）"`
	Hairstyle   string `json:"hairstyle,omitempty"`
	Wardrobe    string `json:"wardrobe,omitempty"`
	Personality string `json:"personality,omitempty"`
	Voice       string `json:"voice,omitempty"`
	FaceRef     string `json:"face_reference_id,omitempty" jsonschema:"顔の参照画像: Reference Library の画像の id（list_references の items[].id。kind=character）"`
	BodyRef     string `json:"body_reference_id,omitempty" jsonschema:"全身の参照画像: Reference Library の画像の id（kind=character）"`
	RefProject  string `json:"reference_project_id,omitempty" jsonschema:"series_id のとき、face_reference_id / body_reference_id の画像を登録した Project（Series には Library がない）。参照画像を渡すなら必須"`
}

// Output is the Character that was made.
type Output struct {
	ProjectID   string                `json:"project_id,omitempty"`
	SeriesID    string                `json:"series_id,omitempty"`
	CharacterID string                `json:"character_id" jsonschema:"作った登場人物の id（assign_character_reference の entity_id、update_series_character・update_character_voice_reference の character_id）"`
	Character   domain.WorldCharacter `json:"character"`
	Version     int                   `json:"version" jsonschema:"作ったあとの World State（Project）または正本（Series）の最新バージョン番号"`
}

func create(svc *service.Services) mcpkit.Handler[Input, Output] {
	return func(ctx context.Context, in Input) (Output, error) {
		if (in.ProjectID == "") == (in.SeriesID == "") {
			return Output{}, invalid("project_id", "project_id か series_id のどちらか一方を指定してください")
		}
		c := voicerefsvc.NewCharacter{
			Name: in.Name, Hairstyle: in.Hairstyle, Wardrobe: in.Wardrobe, Voice: in.Voice, Personality: in.Personality,
			FaceReferenceID: in.FaceRef, BodyReferenceID: in.BodyRef,
		}
		if in.SeriesID != "" {
			return createInSeries(ctx, svc, in, c)
		}
		if in.RefProject != "" {
			return Output{}, invalid("reference_project_id", "reference_project_id は series_id のときだけ使います（Project の画像は、その Project の Library から選びます）")
		}
		id, err := svc.VoiceRef.CreateForProject(ctx, in.ProjectID, in.BranchID, c)
		if err != nil {
			return Output{}, serviceError(err)
		}
		view, err := svc.World.View(ctx, in.ProjectID, in.BranchID)
		if err != nil {
			return Output{}, err
		}
		out := Output{ProjectID: in.ProjectID, CharacterID: id, Version: view.World.Version}
		for _, w := range view.World.Characters {
			if w.ID == id {
				out.Character = w
			}
		}
		return out, nil
	}
}

func createInSeries(ctx context.Context, svc *service.Services, in Input, c voicerefsvc.NewCharacter) (Output, error) {
	if in.BranchID != "" {
		return Output{}, invalid("branch_id", "branch_id は series_id のときだけ使います")
	}
	id, err := svc.VoiceRef.CreateForSeries(ctx, in.SeriesID, in.RefProject, c)
	if err != nil {
		return Output{}, serviceError(err)
	}
	view, err := svc.Series.View(ctx, in.SeriesID)
	if err != nil {
		return Output{}, err
	}
	out := Output{SeriesID: in.SeriesID, CharacterID: id, Version: view.Series.Version}
	for _, w := range view.Canon.Characters {
		if w.ID == id {
			out.Character = w
		}
	}
	return out, nil
}

// serviceError turns a refusal of voicerefsvc into the error of package service the tools report.
func serviceError(err error) error {
	var bad *voicerefsvc.InvalidError
	switch {
	case errors.As(err, &bad):
		return invalid(bad.Field, bad.Message)
	case errors.Is(err, voicerefsvc.ErrNotFound):
		return service.ErrNotFound
	}
	return err
}

func invalid(field, msg string) error { return &service.ValidationError{Field: field, Message: msg} }
