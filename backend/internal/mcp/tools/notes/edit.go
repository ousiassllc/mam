package notes

import (
	"context"
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// UpdateInput names the note and the fields to change; one left out stays as it is.
type UpdateInput struct {
	NoteID   string  `json:"note_id"`
	Title    *string `json:"title,omitempty"`
	Content  *string `json:"content,omitempty"`
	Category *string `json:"category,omitempty" jsonschema:"character, product, location, world, tone, other"`
	Pinned   *bool   `json:"pinned,omitempty"`
	Archived *bool   `json:"archived,omitempty" jsonschema:"true でアーカイブ、false でアーカイブを戻す"`
}

func update(svc *service.NoteService) mcpkit.Handler[UpdateInput, Note] {
	return func(ctx context.Context, in UpdateInput) (Note, error) {
		v, err := svc.Update(ctx, in.NoteID, service.NoteChange{
			Title: in.Title, Content: in.Content, Category: in.Category, Pinned: in.Pinned, Archived: in.Archived,
		})
		if err != nil {
			return Note{}, err
		}
		return pick(v, in.NoteID)
	}
}

// NoteInput names the note.
type NoteInput struct {
	NoteID string `json:"note_id"`
}

func archive(svc *service.NoteService) mcpkit.Handler[NoteInput, Note] {
	return func(ctx context.Context, in NoteInput) (Note, error) {
		v, err := svc.Archive(ctx, in.NoteID)
		if err != nil {
			return Note{}, err
		}
		return pick(v, in.NoteID)
	}
}

// RestoreInput names the note and the version.
type RestoreInput struct {
	NoteID        string `json:"note_id"`
	VersionNumber int    `json:"version_number" jsonschema:"list_*_notes の versions の version_number（1 以上）"`
}

func restore(svc *service.NoteService) mcpkit.Handler[RestoreInput, Note] {
	return func(ctx context.Context, in RestoreInput) (Note, error) {
		number, err := service.ParseVersionNumber(strconv.Itoa(in.VersionNumber))
		if err != nil {
			return Note{}, err
		}
		v, err := svc.Restore(ctx, in.NoteID, number)
		if err != nil {
			return Note{}, err
		}
		return pick(v, in.NoteID)
	}
}
