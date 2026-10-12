package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func noteListData(v service.NotesView) organisms.NoteListData {
	if v.Project.ID != "" {
		return organisms.NoteListData{BasePath: paths.Project(v.Project.ID, "notes"), Notes: v.Notes}
	}
	return organisms.NoteListData{BasePath: paths.Series(v.Series.ID, "notes"), Series: true, Notes: v.Notes}
}

func notesPageData(v service.NotesView) pages.NotesPageData {
	if v.Project.ID != "" {
		return pages.NotesPageData{
			Title: "Notes", Owner: v.Project.Name, BackHref: paths.Project(v.Project.ID), BackLabel: "← チャットに戻る",
			Lead:  "自由記述の制作メモです。ピン留めしたメモは常に、それ以外は関連度の高い順に、脚本・Scene/Shot 計画・Shot Spec の作成に渡されます（アーカイブしたメモは渡されません）。構造化できる内容は、チャットの承認カードで World State への反映を提案します。",
			Notes: noteListData(v),
		}
	}
	return pages.NotesPageData{
		Title: "Series Notes", Owner: v.Series.Name, BackHref: paths.Series(v.Series.ID), BackLabel: "← Series に戻る",
		Lead:  "Series の正本メモです。Episode の開始時に Project へコピーされます（アーカイブ済みは除く）。正本を更新しても、すでに作成した Episode には反映されません。",
		Notes: noteListData(v),
	}
}

func renderNotesPage(c *gin.Context, v service.NotesView) {
	if isHTMX(c) {
		render(c, http.StatusOK, organisms.NoteList(noteListData(v)))
		return
	}
	render(c, http.StatusOK, pages.NotesPage(middleware.CSRFToken(c), notesPageData(v)))
}

// ProjectNotesPage renders the notes of a Project (GET /projects/:id/notes): the full page, or the
// list fragment for HTMX.
func ProjectNotesPage(svc *service.NoteService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.ProjectNotes(c.Request.Context(), c.Param("id"))
		if err != nil {
			pageError(c, err)
			return
		}
		renderNotesPage(c, view)
	}
}

// SeriesNotesPage renders the canonical notes of a Series (GET /series/:id/notes).
func SeriesNotesPage(svc *service.NoteService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.SeriesNotes(c.Request.Context(), c.Param("id"))
		if err != nil {
			pageError(c, err)
			return
		}
		renderNotesPage(c, view)
	}
}

// lastForm returns the last value of a form field (a checkbox is sent after the hidden "false" that
// makes an unchecked box count), and whether the field was sent at all.
func lastForm(c *gin.Context, name string) (string, bool) {
	values := c.PostFormArray(name)
	if len(values) == 0 {
		return "", false
	}
	return values[len(values)-1], true
}

func boolForm(c *gin.Context, name string) *bool {
	v, ok := lastForm(c, name)
	if !ok {
		return nil
	}
	b := v == "true"
	return &b
}

func textForm(c *gin.Context, name string) *string {
	v, ok := c.GetPostForm(name)
	if !ok {
		return nil
	}
	return &v
}

func noteInput(c *gin.Context) service.NoteInput {
	pinned := boolForm(c, "pinned")
	return service.NoteInput{Title: c.PostForm("title"), Content: c.PostForm("content"), Category: c.PostForm("category"), Pinned: pinned != nil && *pinned}
}

// CreateProjectNote adds a note to a Project (POST /projects/:id/notes).
func CreateProjectNote(svc *service.NoteService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.CreateProjectNote(c.Request.Context(), c.Param("id"), noteInput(c))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusCreated, organisms.CreatedNoteList(noteListData(view)))
	}
}

// CreateSeriesNote adds a canonical note to a Series (POST /series/:id/notes).
func CreateSeriesNote(svc *service.NoteService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.CreateSeriesNote(c.Request.Context(), c.Param("id"), noteInput(c))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusCreated, organisms.CreatedNoteList(noteListData(view)))
	}
}

// noteAnswered answers an action on one note with the list of its scope, in which the other notes are
// placeholders: the page keeps them as they are, with the text typed into their forms (#694).
func noteAnswered(c *gin.Context, view service.NotesView) {
	data := noteListData(view)
	data.Only = c.Param("id")
	render(c, http.StatusOK, organisms.NoteGroups(data))
}

// UpdateNote changes the fields that were sent (PUT /notes/:id: title, content, category, pinned,
// archived) and answers with the list of the note's scope (see noteAnswered).
func UpdateNote(svc *service.NoteService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.Update(c.Request.Context(), c.Param("id"), service.NoteChange{
			Title: textForm(c, "title"), Content: textForm(c, "content"), Category: textForm(c, "category"),
			Pinned: boolForm(c, "pinned"), Archived: boolForm(c, "archived"),
		})
		if err != nil {
			toast(c, err)
			return
		}
		noteAnswered(c, view)
	}
}

// ArchiveNote archives a note (DELETE /notes/:id) and answers with the list (see noteAnswered).
func ArchiveNote(svc *service.NoteService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.Archive(c.Request.Context(), c.Param("id"))
		if err != nil {
			toast(c, err)
			return
		}
		noteAnswered(c, view)
	}
}

// RestoreNoteVersion restores an earlier version of a note as a new version
// (POST /notes/:id/versions/:versionNumber/restore) and answers with the list (see noteAnswered).
func RestoreNoteVersion(svc *service.NoteService) gin.HandlerFunc {
	return func(c *gin.Context) {
		number, err := service.ParseVersionNumber(c.Param("versionNumber"))
		if err != nil {
			toast(c, err)
			return
		}
		view, err := svc.Restore(c.Request.Context(), c.Param("id"), number)
		if err != nil {
			toast(c, err)
			return
		}
		noteAnswered(c, view)
	}
}
