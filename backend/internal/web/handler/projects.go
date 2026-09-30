package handler

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
)

// maxUploadRequestBytes caps one multipart request: all attachments plus the text.
const maxUploadRequestBytes = service.MaxAttachments*service.MaxVideoBytes + 1<<20

// multipartMemory is how much of an upload Go keeps in memory before spilling to temp files.
const multipartMemory = 8 << 20

// Projects renders the project list and the new-project form (GET / and GET /projects).
// HTMX requests get the list fragment only.
func Projects(svc *service.ProjectService) gin.HandlerFunc {
	return func(c *gin.Context) {
		projects, err := svc.List(c.Request.Context())
		if err != nil {
			pageError(c, err)
			return
		}
		if isHTMX(c) {
			render(c, http.StatusOK, organisms.ProjectList(projects))
			return
		}
		templates, err := svc.Templates(c.Request.Context())
		if err != nil {
			pageError(c, err)
			return
		}
		render(c, http.StatusOK, pages.ProjectsPage(middleware.CSRFToken(c), projects, organisms.NewProjectFormData{Templates: templates}))
	}
}

// CreateProject starts a Project from the first message (POST /projects). On success the
// browser is sent to the new Chat Workspace; an invalid input re-renders the form with the error.
func CreateProject(svc *service.ProjectService) gin.HandlerFunc {
	return func(c *gin.Context) {
		content, uploads, err := readMessageForm(c)
		if err == nil {
			var project domain.Project
			if project, err = svc.Create(c.Request.Context(), service.CreateProjectInput{Content: content, Uploads: uploads, BrandTemplateID: c.PostForm("brand_template_id")}); err == nil {
				c.Header("HX-Redirect", "/projects/"+project.ID)
				c.Status(http.StatusCreated)
				return
			}
		}
		status, message := statusOf(c, err)
		templates, _ := svc.Templates(c.Request.Context()) // the form is still usable without the choice
		render(c, status, organisms.NewProjectForm(organisms.NewProjectFormData{Content: content, Error: message, Templates: templates}))
	}
}

// readMessageForm reads the content and the attachments of a multipart message form.
func readMessageForm(c *gin.Context) (string, []service.Upload, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadRequestBytes)
	if err := c.Request.ParseMultipartForm(multipartMemory); err != nil {
		return "", nil, &service.ValidationError{Field: "attachments", Message: "送信内容が大きすぎるか、形式が正しくありません"}
	}
	var uploads []service.Upload
	for _, fh := range c.Request.MultipartForm.File["attachments"] {
		if fh.Filename == "" { // an empty file input is sent as an unnamed empty part
			continue
		}
		uploads = append(uploads, uploadOf(fh))
	}
	return c.PostForm("content"), uploads, nil
}

func uploadOf(fh *multipart.FileHeader) service.Upload {
	return service.Upload{
		Name: fh.Filename,
		Size: fh.Size,
		Open: func() (io.ReadCloser, error) { return fh.Open() },
	}
}

// ProjectAsset serves an uploaded file of the Project (GET /projects/:id/assets/:assetId).
func ProjectAsset(svc *service.ProjectService) gin.HandlerFunc {
	return func(c *gin.Context) {
		file, err := svc.Asset(c.Request.Context(), c.Param("id"), c.Param("assetId"))
		if err != nil {
			status, _ := statusOf(c, err)
			c.AbortWithStatus(status)
			return
		}
		c.Header("Content-Type", file.Asset.MimeType)
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Content-Security-Policy", "sandbox")
		c.File(file.Path)
	}
}

// BriefPage is the Creative Brief in the auxiliary view (GET /projects/:id/brief).
func BriefPage(svc *service.ProjectService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.Brief(c.Request.Context(), c.Param("id"))
		if err != nil {
			pageError(c, err)
			return
		}
		data := briefFormData(view, nil, false)
		if isHTMX(c) {
			render(c, http.StatusOK, organisms.BriefForm(data))
			return
		}
		render(c, http.StatusOK, pages.BriefPage(middleware.CSRFToken(c), data))
	}
}

// UpdateBrief saves the edited Brief (PUT /projects/:id/brief). Invalid input
// re-renders the form with the user's input and the error (422).
func UpdateBrief(svc *service.ProjectService) gin.HandlerFunc {
	return func(c *gin.Context) {
		form := service.BriefForm{
			Values:                map[domain.BriefField]string{},
			AdditionalInstruction: c.PostForm("additional_instruction"),
			References:            c.PostForm("references"),
		}
		for _, f := range domain.RequiredBriefFields() {
			form.Values[f] = c.PostForm(string(f))
		}
		view, err := svc.UpdateBrief(c.Request.Context(), c.Param("id"), form)
		if err == nil {
			render(c, http.StatusOK, organisms.BriefForm(briefFormData(view, nil, true)))
			return
		}
		if invalid := asValidation(err); invalid != nil {
			data := briefFormData(view, invalid, false)
			data.ReferencesText = form.References
			render(c, http.StatusUnprocessableEntity, organisms.BriefForm(data))
			return
		}
		toast(c, err)
	}
}

func briefFormData(view service.BriefView, invalid *service.ValidationError, saved bool) organisms.BriefFormData {
	d := organisms.BriefFormData{Project: view.Project, Brief: view.Brief, Assets: view.Assets, Saved: saved}
	if invalid != nil {
		d.Errors = map[string]string{invalid.Field: invalid.Message}
	}
	return d
}

// SetConfirmationLevel changes the Confirmation Level (PUT /projects/:id/confirmation-level)
// and returns the refreshed control.
func SetConfirmationLevel(svc *service.ProjectService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var checkpoints []domain.Checkpoint
		for _, v := range c.PostFormArray("checkpoints") {
			checkpoints = append(checkpoints, domain.Checkpoint(v))
		}
		project, err := svc.SetConfirmationLevel(c.Request.Context(), c.Param("id"),
			domain.ConfirmationLevel(c.PostForm("level")), checkpoints)
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, molecules.ConfirmationControl(project))
	}
}

// pageError renders a failed page load: 404 for an unknown target.
func pageError(c *gin.Context, err error) {
	status, message := statusOf(c, err)
	if isHTMX(c) {
		c.Header("HX-Reswap", "none")
		render(c, status, atoms.ToastOOB(message))
		return
	}
	render(c, status, pages.ErrorPage(middleware.CSRFToken(c), status, message))
}

func asValidation(err error) *service.ValidationError {
	var v *service.ValidationError
	if errors.As(err, &v) {
		return v
	}
	return nil
}
