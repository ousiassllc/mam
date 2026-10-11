package testenv_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/testenv"
)

func TestMissingFFmpeg_SkipsOutsideCI(t *testing.T) {
	t.Setenv("CI", "")
	skipped := true
	t.Run("inner", func(t *testing.T) {
		defer func() { skipped = t.Skipped() }()
		testenv.MissingFFmpeg(t, "ffmpeg is not installed")
	})
	assert.True(t, skipped)
}

func TestMissingFFmpeg_FailsInCI(t *testing.T) {
	t.Setenv("CI", "true")
	var failed bool
	fake := &recorder{TB: t}
	func() {
		defer func() { _ = recover() }()
		testenv.MissingFFmpeg(fake, "ffmpeg is not installed")
	}()
	failed = fake.fatal
	assert.True(t, failed)
}

// recorder records Fatalf instead of failing the real test, and stops like Fatalf does.
type recorder struct {
	testing.TB
	fatal bool
}

func (r *recorder) Fatalf(string, ...any) {
	r.fatal = true
	panic("fatal")
}
func (r *recorder) Skip(...any) { panic("skip") }
