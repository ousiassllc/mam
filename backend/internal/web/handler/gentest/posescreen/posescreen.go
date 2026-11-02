// Package posescreen holds the handlers of the pose picker of the generation test (Issue #986): the body of its dialog (the gallery of the
// pose library, or the offer to install it), the poses that match the filters, the install of the library and the pictures. The pictures are
// the files of poses.Library, never a path the browser names. The page is a thin layer over service/gentest/poses (a separate package of the
// handlers for the directory's line limit).
package posescreen

import (
	"errors"
	"net/http"
	"slices"
	"strings"

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
	p, angle, ok := svc.Poses.ImageOwner(pictureID)
	if !ok {
		return view.Ref{}, false
	}
	return view.Ref{
		Value: ValuePrefix + pictureID, Label: "ポーズ " + p.Name + "（" + angleLabel(angle) + "）", Role: gentest.RolePose, ThumbURL: ImageURL(pictureID),
	}, true
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

// filters are what the gallery is filtered by.
type filters struct {
	text, series, class string
	angle               poses.Angle
}

// classValue is the value of an option of the select of categories: "<series>/" for a whole series, "<series>/<category>" for one.
func classValue(series, class string) string { return series + "/" + class }

// queryOf reads the filters of the request; what is not understood is "no filter" (the first angle is the front).
func queryOf(c *gin.Context) filters {
	f := filters{text: strings.TrimSpace(c.Query(gentestposes.FieldQuery)), angle: poses.Front}
	if series, class, ok := strings.Cut(c.Query(gentestposes.FieldClass), "/"); ok {
		for _, s := range poses.Series {
			if s.ID == series {
				f.series, f.class = series, class
			}
		}
	}
	for _, a := range poses.Angles {
		if string(a.Angle) == c.Query(gentestposes.FieldAngle) {
			f.angle = a.Angle
		}
	}
	return f
}

func galleryOf(all []poses.Pose, f filters, notes gentestposes.Notes) gentestposes.Gallery {
	g := gentestposes.Gallery{Notes: notes, Query: f.text, All: gentestposes.Option{Value: "", Label: "すべて", Selected: f.series == ""}, Results: resultsOf(all, f)}
	for _, s := range poses.Series {
		classes := poses.Classes(all, s.ID)
		if len(classes) == 0 {
			continue
		}
		grp := gentestposes.ClassGroup{Label: s.Label, Options: []gentestposes.Option{
			{Value: classValue(s.ID, ""), Label: s.Label + "（すべて）", Selected: f.series == s.ID && f.class == ""},
		}}
		for _, cl := range classes {
			grp.Options = append(grp.Options, gentestposes.Option{Value: classValue(s.ID, cl), Label: cl, Selected: f.series == s.ID && f.class == cl})
		}
		g.Classes = append(g.Classes, grp)
	}
	for _, a := range poses.Angles {
		g.Angles = append(g.Angles, gentestposes.Option{Value: string(a.Angle), Label: a.Label, Selected: a.Angle == f.angle})
	}
	return g
}

func resultsOf(all []poses.Pose, f filters) gentestposes.Results {
	r := gentestposes.Results{Total: len(all)}
	for _, p := range poses.Filter(all, poses.Query{Text: f.text, Series: f.series, Class: f.class}) {
		angle := p.FirstAngle(f.angle)
		pic := p.Image(angle)
		if pic == "" {
			continue
		}
		r.Cards = append(r.Cards, gentestposes.Card{
			Name: p.Name, NameEN: p.NameEN, Class: p.Class, Image: ImageURL(pic), Angle: angleLabel(angle), Value: ValuePrefix + pic, Other: angle != f.angle,
		})
	}
	return r
}
