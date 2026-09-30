package middleware

import (
	"errors"
	"mime"
	"net/http"

	"github.com/gin-gonic/gin"
)

// MaxBodyBytes is the largest JSON or urlencoded body a route reads. It matches Huma's default
// limit, so the gin routes and /api/v1 agree.
const MaxBodyBytes = 1 << 20

// MaxBody caps the request body at MaxBodyBytes for every route except multipart uploads, whose
// handlers set their own (larger) limit. A urlencoded form is parsed here, because gin ignores a
// form parse error and a too-long form would silently arrive empty; over the limit it is a 413.
func MaxBody() gin.HandlerFunc {
	return func(c *gin.Context) {
		mediaType, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if mediaType == "multipart/form-data" || c.Request.Body == nil || c.Request.Body == http.NoBody {
			c.Next()
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxBodyBytes)
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
