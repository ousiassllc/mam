package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
)

// ExportBundle downloads the Project as a portable bundle (GET /projects/:id/bundle): a ZIP with
// the Project Data Model and its asset files (spec/requirements/non-functional.md §8).
func ExportBundle(svc *service.BundleService) gin.HandlerFunc {
	return func(c *gin.Context) {
		bundle, err := svc.Prepare(c.Request.Context(), c.Param("id"))
		if err != nil {
			status, _ := statusOf(c, err)
			c.AbortWithStatus(status)
			return
		}
		c.Header("Content-Type", "application/zip")
		c.Header("Content-Disposition", `attachment; filename="`+bundle.FileName+`"`)
		c.Header("X-Content-Type-Options", "nosniff")
		c.Status(http.StatusOK)
		if err := bundle.Write(c.Writer); err != nil {
			// The status is sent already: log it and cut the download short (the archive is incomplete).
			_ = c.Error(err)
			c.Abort()
		}
	}
}

// ImportBundle creates a Project from a bundle (POST /projects/import, multipart field "bundle").
// On success the browser goes to the Project; an unusable bundle re-renders the form with the reason
// and a Project that already exists is refused with 409.
func ImportBundle(svc *service.BundleService) gin.HandlerFunc {
	return func(c *gin.Context) {
		project, err := importBundle(c, svc)
		if err != nil {
			status, message := statusOf(c, err)
			if errors.Is(err, service.ErrConflict) {
				message = "このプロジェクトはすでにあります（同じ ID のプロジェクトは読み込めません）"
			}
			render(c, status, organisms.ProjectImportForm(organisms.ProjectImportFormData{Error: message}))
			return
		}
		c.Header("HX-Redirect", "/projects/"+project.ID)
		c.Status(http.StatusCreated)
	}
}

func importBundle(c *gin.Context, svc *service.BundleService) (domain.Project, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, service.MaxBundleBytes+1<<20)
	if err := c.Request.ParseMultipartForm(multipartMemory); err != nil {
		return domain.Project{}, &service.ValidationError{Field: "bundle", Message: "送信内容が大きすぎるか、形式が正しくありません"}
	}
	files := c.Request.MultipartForm.File["bundle"]
	if len(files) == 0 || files[0].Filename == "" {
		return domain.Project{}, &service.ValidationError{Field: "bundle", Message: "バンドルファイルを選んでください"}
	}
	f, err := files[0].Open()
	if err != nil {
		return domain.Project{}, err
	}
	defer func() { _ = f.Close() }()
	return svc.Import(c.Request.Context(), f, files[0].Size)
}
