package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
)

func libraryData(v service.LibraryView) organisms.ReferenceLibraryData {
	return organisms.ReferenceLibraryData{Project: v.Project, Q: v.Query.Q, Kind: v.Query.Kind, Tag: v.Query.Tag, Items: v.Items}
}

// ReferenceLibraryPage renders the Reference Library (GET /projects/:id/references?q=&kind=&tag=): the
// full page, or the view fragment for HTMX.
func ReferenceLibraryPage(svc *service.ReferenceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.Library(c.Request.Context(), c.Param("id"), service.LibraryQuery{Q: c.Query("q"), Kind: c.Query("kind"), Tag: c.Query("tag")})
		if err != nil {
			pageError(c, err)
			return
		}
		if isHTMX(c) {
			render(c, http.StatusOK, organisms.ReferenceLibrary(libraryData(view)))
			return
		}
		render(c, http.StatusOK, pages.ReferenceLibraryPage(middleware.CSRFToken(c), libraryData(view)))
	}
}

// UploadReference registers an image (POST /projects/:id/references, multipart) and answers with the
// Library (201).
func UploadReference(svc *service.ReferenceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, service.MaxImageBytes+1<<20)
		if err := c.Request.ParseMultipartForm(multipartMemory); err != nil {
			toast(c, &service.ValidationError{Field: "file", Message: "送信内容が大きすぎるか、形式が正しくありません"})
			return
		}
		in := service.UploadReferenceInput{Kind: c.PostForm("kind"), Name: c.PostForm("name"), Tags: c.PostForm("tags")}
		if files := c.Request.MultipartForm.File["file"]; len(files) > 0 && files[0].Filename != "" {
			in.File = uploadOf(files[0])
		}
		view, err := svc.Upload(c.Request.Context(), c.Param("id"), in)
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusCreated, organisms.ReferenceLibrary(libraryData(view)))
	}
}

// DeleteReference takes an image out of the Library (DELETE /projects/:id/references/:referenceId).
func DeleteReference(svc *service.ReferenceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.Remove(c.Request.Context(), c.Param("id"), c.Param("referenceId"))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, organisms.ReferenceLibrary(libraryData(view)))
	}
}

// PinReference adds a Library image to the Reference Pack of a Shot (POST /shots/:id/references) and
// answers with the Shot Inspector.
func PinReference(svc *service.ReferenceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.Pin(c.Request.Context(), c.Param("id"), c.PostForm("reference_id"))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, organisms.ShotInspector(inspectorData(view)))
	}
}
