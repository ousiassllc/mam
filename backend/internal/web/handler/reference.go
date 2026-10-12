package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/shotinspector"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
)

func libraryData(v service.LibraryView) organisms.ReferenceLibraryData {
	return organisms.ReferenceLibraryData{Project: v.Project, Q: v.Query.Q, Kind: v.Query.Kind, Tag: v.Query.Tag, Items: v.Items}
}

// ReferenceLibraryPage renders the Reference Library (GET /projects/:id/references?q=&kind=&tag=): the
// full page, or the list for HTMX.
func ReferenceLibraryPage(svc *service.ReferenceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.Library(c.Request.Context(), c.Param("id"), service.LibraryQuery{Q: c.Query("q"), Kind: c.Query("kind"), Tag: c.Query("tag")})
		if err != nil {
			pageError(c, err)
			return
		}
		if isHTMX(c) {
			render(c, http.StatusOK, organisms.ReferenceLibraryList(libraryData(view)))
			return
		}
		render(c, http.StatusOK, pages.ReferenceLibraryPage(middleware.CSRFToken(c), libraryData(view)))
	}
}

// UploadReference registers an image (POST /projects/:id/references, multipart) and answers with the
// list and the emptied upload form (201).
func UploadReference(svc *service.ReferenceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		form, err := readMultipart(c, service.MaxImageBytes+1<<20)
		if err != nil {
			toast(c, err)
			return
		}
		in := service.UploadReferenceInput{Kind: c.PostForm("kind"), Name: c.PostForm("name"), Tags: c.PostForm("tags"), File: optionalUpload(form, "file")}
		view, err := svc.Upload(c.Request.Context(), c.Param("id"), in)
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusCreated, organisms.UploadedReference(libraryData(view)))
	}
}

// DeleteReference takes an image out of the Library (DELETE /projects/:id/references/:referenceId?q=&kind=&tag= keeps the filter).
func DeleteReference(svc *service.ReferenceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.Remove(c.Request.Context(), c.Param("id"), c.Param("referenceId"), service.LibraryQuery{Q: c.Query("q"), Kind: c.Query("kind"), Tag: c.Query("tag")})
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, organisms.ReferenceLibraryList(libraryData(view)))
	}
}

// PinReference adds a Library image to the Reference Pack of a Shot (POST /shots/:id/references) and
// answers with the Reference Pack only (the spec and the 演出指示 keep what is typed in them).
func PinReference(svc *service.ReferenceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.Pin(c.Request.Context(), c.Param("id"), c.PostForm("reference_id"))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, shotinspector.ReferencePackSection(inspectorData(view)))
	}
}
