package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
)

// TakeComparison renders the Take comparison of a Shot (GET /shots/:id/takes): the full page, or the
// comparison for HTMX. The Takes themselves are loaded by the component from the JSON API.
func TakeComparison(svc *service.TakeService) gin.HandlerFunc {
	return func(c *gin.Context) {
		cmp, err := svc.Compare(c.Request.Context(), c.Param("id"))
		if err != nil {
			pageError(c, err)
			return
		}
		data := organisms.TakeComparisonData{Project: cmp.Project, Shot: cmp.Shot}
		if isHTMX(c) {
			render(c, http.StatusOK, organisms.TakeComparison(data))
			return
		}
		render(c, http.StatusOK, pages.TakeComparisonPage(middleware.CSRFToken(c), data))
	}
}

// SelectTake makes a Take the one its Shot uses (POST /takes/:id/select) and answers with the Shot card.
func SelectTake(svc *service.TakeService) gin.HandlerFunc {
	return func(c *gin.Context) {
		frag, err := svc.Select(c.Request.Context(), c.Param("id"))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, molecules.ShotCard(takeActionCard(c, frag)))
	}
}

// RejectTake excludes a Take (DELETE /takes/:id) and answers with the Shot card.
func RejectTake(svc *service.TakeService) gin.HandlerFunc {
	return func(c *gin.Context) {
		frag, err := svc.Reject(c.Request.Context(), c.Param("id"))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, molecules.ShotCard(takeActionCard(c, frag)))
	}
}

// partialForm is the body of POST /shots/:id/takes/partial: the parts as data, or in words.
type partialForm struct {
	Segments    []domain.TakeSegment `json:"segments"`
	Instruction string               `json:"instruction"`
}

// UsePartialTakes joins parts of the Takes of a Shot into a new Take (POST /shots/:id/takes/partial)
// and answers with the Shot card. The body is JSON ({"segments":[{"take_id","start","end"}]} or
// {"instruction":"Take A の 0-2秒 + Take B の 2-4秒"}), or a form whose "segments" field holds that
// JSON array and whose "instruction" field holds the words.
func UsePartialTakes(svc *service.TakeService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in partialForm
		if strings.HasPrefix(c.ContentType(), "application/json") {
			if err := c.ShouldBindJSON(&in); err != nil {
				toast(c, unreadable(c, err, "segments", "区間の指定を読み取れませんでした"))
				return
			}
		} else {
			in.Instruction = c.PostForm("instruction")
			if raw := c.PostForm("segments"); raw != "" {
				if err := json.Unmarshal([]byte(raw), &in.Segments); err != nil {
					toast(c, &service.ValidationError{Field: "segments", Message: "区間の指定を読み取れませんでした"})
					return
				}
			}
		}
		frag, err := svc.UsePartial(c.Request.Context(), c.Param("id"), service.PartialInput{Segments: in.Segments, Instruction: in.Instruction})
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, molecules.ShotCard(shotCardData(frag)))
	}
}

// TakeVideo serves the preview video of a Take (GET /takes/:id/video): the file, or a 1080p proxy
// of a 4K file. Range requests are supported so that the player can seek.
func TakeVideo(svc *service.TakeService) gin.HandlerFunc {
	return func(c *gin.Context) {
		file, err := svc.Video(c.Request.Context(), c.Param("id"))
		serveMedia(c, file, err)
	}
}

// TakeThumbnail serves a still of the first frame of a Take (GET /takes/:id/thumbnail).
func TakeThumbnail(svc *service.TakeService) gin.HandlerFunc {
	return func(c *gin.Context) {
		file, err := svc.Thumbnail(c.Request.Context(), c.Param("id"))
		serveMedia(c, file, err)
	}
}

func serveMedia(c *gin.Context, file service.MediaFile, err error) {
	if err != nil {
		if c.Request.Context().Err() != nil { // the client went away (a seek, a page change): nobody to answer, nothing to log
			c.Abort()
			return
		}
		status, _ := statusOf(c, err)
		c.AbortWithStatus(status)
		return
	}
	c.Header("Content-Type", file.MimeType)
	c.Header("Content-Security-Policy", "sandbox")
	c.File(file.Path)
}
