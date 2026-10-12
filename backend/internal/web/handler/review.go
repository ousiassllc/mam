package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Team Collaboration (spec/spec.md §27.1, spec/api/endpoints.md §2.12). The owner's routes work on a
// Project; the reviewer's routes work on the Review Packages the instance imported and need no Project.

func reviewListData(v service.ReviewsView) organisms.ReviewListData {
	d := organisms.ReviewListData{ProjectID: v.Project.ID, Exports: v.Exports}
	for _, it := range v.Items {
		d.Items = append(d.Items, organisms.ReviewListItem{Item: it.ReviewItem, Export: it.Export})
	}
	return d
}

// ProjectReviewsPage renders the owner's review screen (GET /projects/:id/reviews): the full page,
// or the list fragment for HTMX.
func ProjectReviewsPage(svc *service.ReviewService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.ProjectReviews(c.Request.Context(), c.Param("id"))
		if err != nil {
			pageError(c, err)
			return
		}
		if isHTMX(c) {
			render(c, http.StatusOK, organisms.ReviewList(reviewListData(view)))
			return
		}
		render(c, http.StatusOK, pages.ReviewsPage(middleware.CSRFToken(c), view.Project, reviewListData(view)))
	}
}

// CreateReview makes a review request from an Export (POST /projects/:id/reviews) and answers with
// the list (201).
func CreateReview(svc *service.ReviewService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.CreateRequest(c.Request.Context(), c.Param("id"), c.PostForm("export_id"), c.PostForm("note"))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusCreated, organisms.ReviewList(reviewListData(view)))
	}
}

// DownloadReviewPackage downloads the Review Package of a review request
// (GET /projects/:id/reviews/:reviewId/package).
func DownloadReviewPackage(svc *service.ReviewService) gin.HandlerFunc {
	return func(c *gin.Context) {
		pkg, err := svc.Package(c.Request.Context(), c.Param("id"), c.Param("reviewId"))
		if err != nil {
			status, _ := statusOf(c, err)
			c.AbortWithStatus(status)
			return
		}
		c.Header("Content-Type", "application/zip")
		c.Header("Content-Disposition", `attachment; filename="`+pkg.FileName+`"`)
		c.Status(http.StatusOK)
		if err := pkg.Write(c.Writer); err != nil {
			abortDownload(c, err)
		}
	}
}

// ImportReviewResponse imports a Review Response file (POST /projects/:id/reviews/feedback, multipart
// field "response") and answers with the list (201).
func ImportReviewResponse(svc *service.ReviewService) gin.HandlerFunc {
	return func(c *gin.Context) {
		form, err := readMultipart(c, service.MaxReviewResponseRequestBytes)
		if err != nil {
			toast(c, err)
			return
		}
		fh, err := firstFile(form, "response", "応答ファイルを選んでください")
		if err != nil {
			toast(c, err)
			return
		}
		f, err := fh.Open()
		if err != nil {
			toast(c, err)
			return
		}
		defer func() { _ = f.Close() }()
		view, err := svc.ImportResponse(c.Request.Context(), c.Param("id"), f)
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusCreated, organisms.ReviewList(reviewListData(view)))
	}
}

// SetReviewCommentResolved marks an imported comment as resolved or open
// (PUT /projects/:id/review-comments/:commentId) and answers with the list.
func SetReviewCommentResolved(svc *service.ReviewService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.SetCommentResolved(c.Request.Context(), c.Param("id"), c.Param("commentId"), c.PostForm("resolved") == "true")
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, organisms.ReviewList(reviewListData(view)))
	}
}

// ReviewInboxPage renders the reviewer's entrance (GET /reviews).
func ReviewInboxPage(svc *service.ReviewService) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := svc.Inbox(c.Request.Context())
		if err != nil {
			pageError(c, err)
			return
		}
		data := organisms.ReviewInboxData{Items: items}
		if isHTMX(c) {
			render(c, http.StatusOK, organisms.ReviewInboxList(data))
			return
		}
		render(c, http.StatusOK, pages.ReviewInboxPage(middleware.CSRFToken(c), data))
	}
}

// ReviewPage renders the review screen of an imported package (GET /reviews/:id).
func ReviewPage(svc *service.ReviewService) gin.HandlerFunc {
	return func(c *gin.Context) {
		i, err := svc.Get(c.Request.Context(), c.Param("id"))
		if err != nil {
			pageError(c, err)
			return
		}
		if isHTMX(c) {
			render(c, http.StatusOK, organisms.ReviewDecision(i))
			return
		}
		render(c, http.StatusOK, pages.ReviewPage(middleware.CSRFToken(c), i))
	}
}

// ReviewVideo serves the video of an imported package (GET /reviews/:id/video).
func ReviewVideo(svc *service.ReviewService) gin.HandlerFunc {
	return func(c *gin.Context) {
		file, err := svc.VideoFile(c.Request.Context(), c.Param("id"))
		serveMedia(c, file, err)
	}
}

// DownloadReviewResponse downloads the Review Response of an imported package
// (GET /reviews/:id/response).
func DownloadReviewResponse(svc *service.ReviewService) gin.HandlerFunc {
	return func(c *gin.Context) {
		file, err := svc.Response(c.Request.Context(), c.Param("id"))
		if err != nil {
			status, _ := statusOf(c, err)
			c.AbortWithStatus(status)
			return
		}
		c.Header("Content-Disposition", `attachment; filename="`+file.FileName+`"`)
		c.Data(http.StatusOK, "application/json", file.Data)
	}
}

// ImportReviewPackage imports a Review Package (POST /reviews/import, multipart field "package").
// On success the browser goes to the review screen (201).
func ImportReviewPackage(svc *service.ReviewService) gin.HandlerFunc {
	return func(c *gin.Context) {
		inbox, err := importReviewPackage(c, svc)
		if err != nil {
			toast(c, err)
			return
		}
		c.Header("HX-Redirect", paths.Review(inbox.ID))
		c.Status(http.StatusCreated)
	}
}

func importReviewPackage(c *gin.Context, svc *service.ReviewService) (domain.ReviewInbox, error) {
	form, err := readMultipart(c, service.MaxReviewPackageBytes+1<<20)
	if err != nil {
		return domain.ReviewInbox{}, err
	}
	fh, err := firstFile(form, "package", "レビューパッケージを選んでください")
	if err != nil {
		return domain.ReviewInbox{}, err
	}
	f, err := fh.Open()
	if err != nil {
		return domain.ReviewInbox{}, err
	}
	defer func() { _ = f.Close() }()
	return svc.ImportPackage(c.Request.Context(), f, fh.Size)
}

// AddReviewComment adds a comment of the reviewer (POST /reviews/:id/comments) and answers with the
// list of the comments and an empty form (201), not the whole screen: that would reset the video.
func AddReviewComment(svc *service.ReviewService) gin.HandlerFunc {
	return func(c *gin.Context) {
		i, err := svc.AddComment(c.Request.Context(), c.Param("id"), c.PostForm("timecode"), c.PostForm("body"))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusCreated, organisms.ReviewCommentAdded(i))
	}
}

// DeleteReviewComment removes a comment of the reviewer (DELETE /reviews/:id/comments/:commentId) and
// answers with the list of the comments.
func DeleteReviewComment(svc *service.ReviewService) gin.HandlerFunc {
	return func(c *gin.Context) {
		i, err := svc.DeleteComment(c.Request.Context(), c.Param("id"), c.Param("commentId"))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, organisms.ReviewComments(i))
	}
}

// DecideReview writes the reviewer's decision (PUT /reviews/:id/decision) and answers with the decision section.
func DecideReview(svc *service.ReviewService) gin.HandlerFunc {
	return func(c *gin.Context) {
		i, err := svc.Decide(c.Request.Context(), c.Param("id"), c.PostForm("reviewer_name"), c.PostForm("decision"), c.PostForm("comment"))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, organisms.ReviewDecision(i))
	}
}
