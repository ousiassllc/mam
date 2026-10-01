package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestIsLocalPath_OnlySameOriginAbsolutePaths(t *testing.T) {
	for _, p := range []string{"/takes/t1/video", "/takes/t%3F1/thumbnail", "/a?b=c"} {
		assert.True(t, domain.IsLocalPath(p), p)
	}
	for _, p := range []string{
		"", "takes/t1/video", "//evil.example/x.mp4", `/\evil.example/x.mp4`, `/%5Cevil.example/x.mp4`, `/%5cevil.example/x.mp4`,
		"/\tevil", "/a\nb", "https://evil.example/x.mp4", "/\x7f",
	} {
		assert.False(t, domain.IsLocalPath(p), p)
	}
}
