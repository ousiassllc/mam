package screentest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/facefusion"
	"github.com/ousiassllc/mam/backend/internal/media/ffmpegdl"
	"github.com/ousiassllc/mam/backend/internal/media/locate"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service/facefusionsettings"
	"github.com/ousiassllc/mam/backend/internal/service/ffmpegsettings"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

// Companion is the companion software of a screen (the page "付随ソフト" of the model manager): ffmpeg with its download, FaceFusion and the Codex engine. A nil one is
// a part the build does not have.
type Companion struct {
	FFmpeg     *ffmpegsettings.Service
	Download   *ffmpegdl.Service
	FaceFusion *facefusionsettings.Service
	// Codex gives the screen a ChatGPT service, whose Codex engine the list reads (nothing is signed in or started).
	Codex bool
}

// NewWithCompanion opens the screen with the companion software c (and none of the other categories' optional services).
func NewWithCompanion(t *testing.T, c Companion) *Screen {
	t.Helper()
	return open(t, mmtest.New(t), nil, nil, companionOf(t, c))
}

// companionOf puts the companion software into the settings routes the screen is built over.
func companionOf(t *testing.T, c Companion) func(*settingsroutes.Services) {
	return func(s *settingsroutes.Services) {
		s.FFmpeg, s.FFmpegDownload, s.FaceFusion = c.FFmpeg, c.Download, c.FaceFusion
		if c.Codex {
			s.ChatGPT = chatgpt.New(chatgpt.Options{Keys: credentialtest.New(), DataDir: t.TempDir(), LockPath: filepath.Join(t.TempDir(), "lock")})
		}
	}
}

// IsolatedFFmpeg is the ffmpeg settings over a file of the test's temporary directory, on a Mac whose PATH has no Homebrew (the Finder launch): only the files of the
// temporary directory count, so the machine's own ffmpeg, if it has one, is not found. It returns the file too; opts add to the service (a download's directory).
func IsolatedFFmpeg(t *testing.T, opts ...ffmpegsettings.Option) (*ffmpegsettings.Service, string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), ffmpegsettings.FileName)
	finder := locate.Finder{GOOS: "darwin", Path: "/usr/bin:/bin", Usable: func(p string) bool {
		return strings.HasPrefix(p, os.TempDir()) && locate.IsExecutable(p)
	}}
	return ffmpegsettings.New(file, append([]ffmpegsettings.Option{ffmpegsettings.WithFinder(finder)}, opts...)...), file
}

// IsolatedFaceFusion is the FaceFusion settings over a file of the test's temporary directory; only the files of the temporary directory count, so the machine's own
// python and FaceFusion are not found (it looks in /nowhere/facefusion). It returns the file too.
func IsolatedFaceFusion(t *testing.T) (*facefusionsettings.Service, string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), facefusionsettings.FileName)
	finder := facefusion.Finder{Finder: locate.Finder{GOOS: "linux", Path: "/nowhere", Usable: func(p string) bool {
		return strings.HasPrefix(p, os.TempDir()) && locate.IsExecutable(p)
	}}, Dirs: []string{"/nowhere/facefusion"}}
	return facefusionsettings.New(file, "", facefusionsettings.WithFinder(finder)), file
}
