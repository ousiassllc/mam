// Package posescreen holds the handlers of the pose picker of the generation test (Issue #986): the body of its dialog (the gallery of the
// pose library, or the offer to install it), the poses that match the filters, the install of the library and the pictures. The pictures are
// the files of poses.Library, never a path the browser names. The page is a thin layer over service/gentest/poses (a separate package of the
// handlers for the directory's line limit).
package posescreen

import (
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/poses"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gtshare"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentestposes"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// ValuePrefix starts the value of an input image of the pose library: "pose_library::<picture id>" (the form's "<source>:<project>:<id>"
// with no project).
const ValuePrefix = gentest.FromPoseLibrary + "::"

// Handlers answer the routes of the picker.
type Handlers struct {
	Svc *gentest.Service
}

// Register adds the routes of the picker to the engine.
func Register(engine *gin.Engine, svc *gentest.Service) {
	h := Handlers{Svc: svc}
	engine.GET(paths.GenerationTestPoses(), h.Panel)
	engine.GET(paths.GenerationTestPoses("results"), h.Results)
	engine.POST(paths.GenerationTestPoses("install"), h.Install)
	engine.POST(paths.GenerationTestPoses("remove"), h.Remove)
	engine.GET(paths.GenerationTestPoses("progress"), h.Progress)
	engine.GET(paths.GenerationTestPoses("image", ":id"), h.Image)
}

// ImageURL is the URL of a picture of the library.
func ImageURL(pictureID string) string { return paths.GenerationTestPoses("image", pictureID) }

// RefOf is the thumbnail row of a picture of the pose library for the list of the chosen input images (false: the library is not there,
// not installed, or has no such picture). The label says which pose and angle it is.
func RefOf(svc *gentest.Service, pictureID string) (view.Ref, bool) {
	if svc.Poses == nil {
		return view.Ref{}, false
	}
	p, angle, kind, ok := svc.Poses.ImageOwner(pictureID)
	if !ok {
		return view.Ref{}, false
	}
	return view.Ref{
		Value: ValuePrefix + pictureID, Label: "ポーズ " + p.Name + "（" + angleLabel(angle) + kindNote(kind) + "）", Role: gentest.RolePose, ThumbURL: ImageURL(pictureID),
	}, true
}

// kindNote is what the label of a picture says after its angle when it is a map for a ControlNet ("" for the line art).
func kindNote(k poses.Kind) string {
	if k == poses.Line {
		return ""
	}
	return "・" + kindLabel(k) + "マップ"
}

func kindLabel(k poses.Kind) string {
	for _, c := range poses.Kinds {
		if c.Kind == k {
			return c.Label
		}
	}
	return string(k)
}

func angleLabel(a poses.Angle) string {
	for _, c := range poses.Angles {
		if c.Angle == a {
			return c.Label
		}
	}
	return string(a)
}

// library is the pose library of the service; false (and a 404) when the app has none.
func (h Handlers) library(c *gin.Context) (*poses.Library, bool) {
	if h.Svc.Poses == nil {
		c.AbortWithStatus(http.StatusNotFound)
		return nil, false
	}
	return h.Svc.Poses, true
}

// notesOf says what the models that are ticked do with a pose picture: those that read it as a pose, with their commercial-use answers.
func notesOf(models []modelcatalog.Info, ticked []string) gentestposes.Notes {
	var n gentestposes.Notes
	for _, m := range models {
		if !m.Available || !slices.Contains(m.ReferenceRoles, gentest.RolePose) {
			continue
		}
		n.Capable = append(n.Capable, m.Label)
	}
	for _, m := range models {
		if !m.Available || !slices.Contains(ticked, m.ID) {
			continue
		}
		n.Ticked++
		if slices.Contains(m.ReferenceRoles, gentest.RolePose) {
			n.Models = append(n.Models, gentestposes.ModelNote{Label: m.Label, Answer: commercial.FromInfo(m.License)})
		}
	}
	return n
}

// notes reads the ticked models out of the request (the media and the models of the compose form, which the dialog sends along).
func (h Handlers) notes(c *gin.Context) gentestposes.Notes {
	models := h.Svc.Models(c.Request.Context(), gentest.MediaImage)
	return notesOf(models, c.QueryArray(view.FieldModel))
}

// notesPost is notes for a POST (the models are in the form of the request).
func (h Handlers) notesPost(c *gin.Context) gentestposes.Notes {
	models := h.Svc.Models(c.Request.Context(), gentest.MediaImage)
	return notesOf(models, c.PostFormArray(view.FieldModel))
}

// Panel answers GET /generation-test/poses: the body of the dialog.
func (h Handlers) Panel(c *gin.Context) {
	lib, ok := h.library(c)
	if !ok {
		return
	}
	h.render(c, lib, http.StatusOK, h.notes(c), "")
}

// render writes the gallery when the library is installed, else the offer to install it (with the message of a failed install).
func (h Handlers) render(c *gin.Context, lib *poses.Library, status int, notes gentestposes.Notes, failure string) {
	w := gentestposes.NewWire()
	st := lib.Status()
	if !st.Installed {
		gtshare.Render(c, status, gentestposes.InstallPanel(w, installOf(st, notes, failure)))
		return
	}
	all, err := lib.Poses()
	if err != nil {
		gtshare.Render(c, http.StatusInternalServerError, gentestposes.InstallPanel(w, installOf(st, notes, "ポーズ集を読み込めませんでした。導入し直してください")))
		return
	}
	gtshare.Render(c, status, gentestposes.GalleryPanel(w, galleryOf(all, queryOf(c), notes)))
}

func installOf(st poses.Status, notes gentestposes.Notes, failure string) gentestposes.Install {
	in := gentestposes.Install{
		Notes: notes, Error: failure, Poses: st.Poses, Images: st.Images, Download: fetch.HumanBytes(uint64(max(st.DownloadBytes, 0))), //nolint:gosec // clamped
		Source: st.Source,
	}
	for _, l := range st.Licenses {
		in.Licenses = append(in.Licenses, gentestposes.LicenseRow{
			Name: l.Name, Summary: l.Summary,
			Answer: commercial.View{Level: l.Commercial, LicenseName: l.Name, SourceURL: l.URL, Notice: l.Notice},
		})
	}
	return in
}

// Install answers POST /generation-test/poses/install: the library is downloaded and verified (the box must be ticked: the service refuses
// without it) and the body of the dialog comes back: the gallery, or the offer again with what went wrong.
func (h Handlers) Install(c *gin.Context) {
	lib, ok := h.library(c)
	if !ok {
		return
	}
	accepted := slices.Contains([]string{"on", "1", "true"}, c.PostForm(gentestposes.FieldAccept))
	if err := lib.Install(c.Request.Context(), accepted, nil); err != nil {
		if c.Request.Context().Err() != nil {
			return // the user left: what was downloaded stays for the next try
		}
		status, msg := refuse(c, err)
		h.render(c, lib, status, h.notesPost(c), msg)
		return
	}
	h.render(c, lib, http.StatusOK, h.notesPost(c), "")
}

// Remove answers POST /generation-test/poses/remove: the pictures and the catalog are deleted and the body of the dialog comes back as the
// offer to install (a library that is already gone is the same answer; a running install is refused: 409 and its message).
func (h Handlers) Remove(c *gin.Context) {
	lib, ok := h.library(c)
	if !ok {
		return
	}
	if err := lib.Remove(); err != nil {
		status, msg := refuse(c, err) // the gallery stays as it is: the message is the answer, as for the filters
		c.String(status, msg)
		return
	}
	h.render(c, lib, http.StatusOK, h.notesPost(c), "")
}

// Progress answers GET /generation-test/poses/progress: how far the install is, as the line the install panel polls ("" when none runs).
func (h Handlers) Progress(c *gin.Context) {
	lib, ok := h.library(c)
	if !ok {
		return
	}
	p := lib.Progress()
	text := ""
	if p.Running && p.Total > 0 {
		text = fmt.Sprintf("ダウンロード中: %s / %s（%d%%）", fetch.HumanBytes(uint64(max(p.Done, 0))), fetch.HumanBytes(uint64(p.Total)), min(100, p.Done*100/p.Total)) //nolint:gosec // non-negative
	} else if p.Running {
		text = "ダウンロードの準備をしています…"
	}
	gtshare.Render(c, http.StatusOK, gentestposes.ProgressText(text))
}

// refuse is the status and message of a failed install (gtshare.Refuse; a library that is not installed is the user's to fix: 422).
func refuse(c *gin.Context, err error) (int, string) {
	if errors.Is(err, poses.ErrNotInstalled) {
		return http.StatusUnprocessableEntity, "ポーズ集が導入されていません"
	}
	return gtshare.Refuse(c, err)
}

// Results answers GET /generation-test/poses/results: the poses that match the filters ("?pose_q=&pose_class=&pose_angle=").
func (h Handlers) Results(c *gin.Context) {
	lib, ok := h.library(c)
	if !ok {
		return
	}
	all, err := lib.Poses()
	if err != nil {
		status, msg := refuse(c, err)
		c.String(status, msg)
		return
	}
	gtshare.Render(c, http.StatusOK, gentestposes.ResultsList(gentestposes.NewWire(), resultsOf(all, queryOf(c))))
}

// Image answers GET /generation-test/poses/image/:id: a picture of an installed pose (404 for anything else).
func (h Handlers) Image(c *gin.Context) {
	lib, ok := h.library(c)
	if !ok {
		return
	}
	path, err := lib.File(c.Param("id"))
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	c.Header("Content-Type", "image/webp")
	c.Header("Cache-Control", "private, max-age=3600")
	c.Header("X-Content-Type-Options", "nosniff")
	c.File(path)
}
