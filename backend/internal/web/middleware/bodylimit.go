package middleware

import (
	"errors"
	"mime"
	"net/http"

	"github.com/gin-gonic/gin"
)

// MaxBodyBytes is the largest body a route reads, unless the route is an upload that sets its own
// limit in readMultipart. It matches Huma's default limit, so the gin routes and /api/v1 agree.
const MaxBodyBytes = 1 << 20

// uploadRoutes are the routes whose handlers call readMultipart and therefore apply their own,
// larger MaxBytesReader. Every other route keeps MaxBodyBytes no matter the Content-Type.
var uploadRoutes = map[string]struct{}{
	"POST /projects":                      {},
	"POST /projects/:id/messages":         {},
	"POST /projects/import":               {},
	"POST /projects/:id/references":       {},
	"POST /projects/:id/audio/voices":     {},
	"POST /projects/:id/reviews/feedback": {},
	"POST /reviews/import":                {},
}

func isUploadRoute(c *gin.Context) bool {
	_, ok := uploadRoutes[c.Request.Method+" "+c.FullPath()]
	return ok
}

// MaxBody caps the request body at MaxBodyBytes. Upload routes are the exception: they limit the
// body themselves. A declared length over the cap is refused before the body is read, so it is not
// parsed into memory or a temp file. A urlencoded form within the cap is parsed here, because gin
// ignores a form parse error and a too-long form would silently arrive empty.
func MaxBody() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body == nil || c.Request.Body == http.NoBody || isUploadRoute(c) {
			c.Next()
			return
		}
		if c.Request.ContentLength > MaxBodyBytes {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "送信内容が大きすぎます"})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxBodyBytes)
		mediaType, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if mediaType == "application/x-www-form-urlencoded" {
			if err := c.Request.ParseForm(); err != nil {
				status := http.StatusBadRequest
				if isTooLarge(err) {
					status = http.StatusRequestEntityTooLarge
				}
				c.AbortWithStatusJSON(status, gin.H{"error": http.StatusText(status)})
				return
			}
		}
		c.Next()
	}
}

func isTooLarge(err error) bool {
	var tooLarge *http.MaxBytesError
	return errors.As(err, &tooLarge)
}
