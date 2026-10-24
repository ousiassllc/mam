// Package notes holds the MCP tools of the Notes screens (/projects/:id/notes, /series/:id/notes): listing,
// creating, editing, archiving and restoring an earlier version of the free-text notes of a Project and the
// canonical notes of a Series.
package notes

import (
	"context"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/worldcommon"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Register adds the tools of this package.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	n := svc.Notes
	mcpkit.Read(r, "list_project_notes",
		"Project のメモ一覧（アーカイブ済みを含む。ピン留めが先、次に更新が新しい順。各メモにバージョン履歴つき）/ The notes of a Project with their version history.",
		listProject(n))
	mcpkit.Read(r, "list_series_notes",
		"Series の正本メモの一覧（アーカイブ済みを含む）。Episode の開始時に Project へコピーされる / The canonical notes of a Series.",
		listSeries(n))
	mcpkit.Write(r, "create_project_note",
		"Project にメモを作る。category を省略すると AI が分類する（モデル未設定なら other）。作成後に AI が World State への反映を提案することがある / Add a note to a Project.",
		createProject(n))
	mcpkit.Write(r, "create_series_note",
		"Series の正本メモを作る（既存の Episode には反映されない）/ Add a canonical note to a Series.",
		createSeries(n))
	mcpkit.Write(r, "update_note",
		"メモを、渡した項目だけ更新する（title・content が変わると新しいバージョンになる。category・pinned・archived はメモだけを更新）。archived=false でアーカイブを戻せる / Update the given fields of a note.",
		update(n), mcpkit.Idempotent())
	mcpkit.Write(r, "archive_note",
		"メモをアーカイブする（一覧には残るが、どの AI にも渡されなくなる）/ Archive a note: it stays listed but is no longer given to any Agent.",
		archive(n), mcpkit.Idempotent())
	mcpkit.Write(r, "restore_note_version",
		"メモの以前のバージョンの題・本文を、最新バージョンとして追加する / Restore an earlier version of a note as a new version.",
		restore(n))
}

// Note is a note with its latest version.
type Note struct {
	ID            string                     `json:"id"`
	ProjectID     string                     `json:"project_id,omitempty"`
	SeriesID      string                     `json:"series_id,omitempty"`
	OriginID      string                     `json:"origin_id,omitempty" jsonschema:"Project のメモのコピー元の Series メモ"`
	Category      string                     `json:"category" jsonschema:"character, product, location, world, tone, other"`
	Pinned        bool                       `json:"pinned" jsonschema:"ピン留めしたメモは常に AI に渡される"`
	Archived      bool                       `json:"archived"`
	ArchivedAt    *time.Time                 `json:"archived_at,omitempty"`
	Title         string                     `json:"title"`
	Content       string                     `json:"content"`
	VersionNumber int                        `json:"version_number" jsonschema:"最新のバージョン番号"`
	Source        string                     `json:"source" jsonschema:"最新バージョンの出所: manual か chat"`
	CreatedAt     time.Time                  `json:"created_at"`
	UpdatedAt     time.Time                  `json:"updated_at" jsonschema:"最新バージョンを保存した時刻"`
	Versions      []worldcommon.VersionEntry `json:"versions" jsonschema:"バージョン履歴（新しい順、summary は題）。restore_note_version に version_number を渡す"`
}

func noteOf(n domain.NoteView) Note {
	return Note{
		ID: n.ID, ProjectID: n.ProjectID, SeriesID: n.SeriesID, OriginID: n.OriginID, Category: string(n.Category),
		Pinned: n.Pinned, Archived: n.Archived(), ArchivedAt: n.ArchivedAt, Title: n.Version.Title, Content: n.Version.Content,
		VersionNumber: n.Version.Number, Source: string(n.Version.Source), CreatedAt: n.CreatedAt, UpdatedAt: n.Version.CreatedAt,
		Versions: worldcommon.VersionEntries(n.Versions),
	}
}

// List is the notes of a Project or of a Series.
type List struct {
	ProjectID string `json:"project_id,omitempty"`
	SeriesID  string `json:"series_id,omitempty"`
	Notes     []Note `json:"notes"`
}

func listOf(v service.NotesView) List {
	out := List{ProjectID: v.Project.ID, SeriesID: v.Series.ID, Notes: make([]Note, 0, len(v.Notes))}
	for _, n := range v.Notes {
		out.Notes = append(out.Notes, noteOf(n))
	}
	return out
}

// ProjectInput names the Project.
type ProjectInput struct {
	ProjectID string `json:"project_id"`
}

// SeriesInput names the Series.
type SeriesInput struct {
	SeriesID string `json:"series_id"`
}

func listProject(svc *service.NoteService) mcpkit.Handler[ProjectInput, List] {
	return func(ctx context.Context, in ProjectInput) (List, error) {
		v, err := svc.ProjectNotes(ctx, in.ProjectID)
		return listOf(v), err
	}
}

func listSeries(svc *service.NoteService) mcpkit.Handler[SeriesInput, List] {
	return func(ctx context.Context, in SeriesInput) (List, error) {
		v, err := svc.SeriesNotes(ctx, in.SeriesID)
		return listOf(v), err
	}
}

// Body is the text of a note to create.
type Body struct {
	Title    string `json:"title,omitempty" jsonschema:"題（100 文字以内）。省略すると本文の 1 行目"`
	Content  string `json:"content" jsonschema:"本文（1〜4000 文字）"`
	Category string `json:"category,omitempty" jsonschema:"character, product, location, world, tone, other。省略すると AI が分類する"`
	Pinned   bool   `json:"pinned,omitempty"`
}

func (b Body) input() service.NoteInput {
	return service.NoteInput{Title: b.Title, Content: b.Content, Category: b.Category, Pinned: b.Pinned}
}

// CreateProjectInput is a note of a Project.
type CreateProjectInput struct {
	ProjectID string `json:"project_id"`
	Body
}

// CreateSeriesInput is a canonical note of a Series.
type CreateSeriesInput struct {
	SeriesID string `json:"series_id"`
	Body
}

func createProject(svc *service.NoteService) mcpkit.Handler[CreateProjectInput, Note] {
	return func(ctx context.Context, in CreateProjectInput) (Note, error) {
		return created(svc.ProjectNotes(ctx, in.ProjectID))(svc.CreateProjectNote(ctx, in.ProjectID, in.input()))
	}
}

func createSeries(svc *service.NoteService) mcpkit.Handler[CreateSeriesInput, Note] {
	return func(ctx context.Context, in CreateSeriesInput) (Note, error) {
		return created(svc.SeriesNotes(ctx, in.SeriesID))(svc.CreateSeriesNote(ctx, in.SeriesID, in.input()))
	}
}

// created answers with the note a Create call made. The service answers with the whole list, so the
// new note is the one that was not in the list taken before (before, beforeErr) the call.
func created(before service.NotesView, beforeErr error) func(after service.NotesView, err error) (Note, error) {
	return func(after service.NotesView, err error) (Note, error) {
		if beforeErr != nil {
			return Note{}, beforeErr
		}
		if err != nil {
			return Note{}, err
		}
		known := make(map[string]bool, len(before.Notes))
		for _, n := range before.Notes {
			known[n.ID] = true
		}
		for _, n := range after.Notes {
			if !known[n.ID] {
				return noteOf(n), nil
			}
		}
		return Note{}, service.ErrNotFound
	}
}

func pick(v service.NotesView, id string) (Note, error) {
	for _, n := range v.Notes {
		if n.ID == id {
			return noteOf(n), nil
		}
	}
	return Note{}, service.ErrNotFound
}
