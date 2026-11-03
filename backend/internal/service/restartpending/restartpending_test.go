package restartpending_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/service/restartpending"
)

func TestRegistry_ListsEntriesInKeyOrderAndClears(t *testing.T) {
	r := restartpending.New()
	assert.Empty(t, r.Reasons())

	r.Set("llm", []string{"a", "b"})
	r.Set("ffmpeg", []string{"c"})
	assert.Equal(t, []string{"c", "a", "b"}, r.Reasons())

	r.Set("llm", nil)
	assert.Equal(t, []string{"c"}, r.Reasons())
	r.Clear("ffmpeg")
	assert.Empty(t, r.Reasons())
}

func TestRegistry_NilIsEmptyAndIgnoresWrites(t *testing.T) {
	var r *restartpending.Registry
	r.Set("x", []string{"y"})
	assert.Empty(t, r.Reasons())
}
