package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestTakeMediaURLs_EscapeAnImportedTakeID(t *testing.T) {
	assert.Equal(t, "/takes/t1/video", domain.TakeVideoURL("t1"))
	assert.Equal(t, "/takes/a%3Fb%23c%25/video", domain.TakeVideoURL("a?b#c%"))
	assert.Equal(t, "/takes/a%3Fb%23c%25/thumbnail", domain.TakeThumbnailURL("a?b#c%"))
}
