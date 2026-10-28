package onboarding

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newService(t *testing.T, opts ...Option) (*Service, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), FileName)
	return New(path, opts...), path
}

func TestCompleted_IsFalseOnTheFirstRun(t *testing.T) {
	s, path := newService(t)

	done, err := s.Completed()

	require.NoError(t, err)
	assert.False(t, done)
	assert.NoFileExists(t, path, "reading the flag creates nothing")
}

func TestComplete_SetsTheFlagAndKeepsItWhenCalledAgain(t *testing.T) {
	s, path := newService(t)

	require.NoError(t, s.Complete())
	require.NoError(t, s.Complete())

	done, err := s.Completed()
	require.NoError(t, err)
	assert.True(t, done)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.JSONEq(t, `{"completed": true}`, string(raw))
}

func TestComplete_KeepsTheOtherKeysOfTheFile(t *testing.T) {
	s, path := newService(t)
	require.NoError(t, os.WriteFile(path, []byte(`{"note": "by hand"}`), 0o600))

	require.NoError(t, s.Complete())

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.JSONEq(t, `{"note": "by hand", "completed": true}`, string(raw))
}

func TestCompleted_ReadsAFlagThatWasSetToFalse(t *testing.T) {
	s, path := newService(t)
	require.NoError(t, os.WriteFile(path, []byte(`{"completed": false}`), 0o600))

	done, err := s.Completed()

	require.NoError(t, err)
	assert.False(t, done)
}

func TestCompleted_RefusesAFileThatIsNotTheFlag(t *testing.T) {
	for name, content := range map[string]string{
		"not json":      `{`,
		"not a boolean": `{"completed": "yes"}`,
	} {
		t.Run(name, func(t *testing.T) {
			s, path := newService(t)
			require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

			_, err := s.Completed()

			require.Error(t, err)
		})
	}
}

func TestComplete_FailsWhenTheFileCannotBeWritten(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))
	s := New(filepath.Join(blocker, FileName)) // a directory that is a file

	require.Error(t, s.Complete())
}

func TestOS(t *testing.T) {
	tests := map[string]OS{"windows": OSWindows, "darwin": OSMac, "linux": OSOther, "freebsd": OSOther}
	for goos, want := range tests {
		s, _ := newService(t, WithGOOS(goos))
		assert.Equal(t, want, s.OS(), goos)
	}
}

func TestFFmpeg_ReportsWhichToolsWereFound(t *testing.T) {
	look := func(have ...string) Option {
		return WithLookPath(func(file string) (string, error) {
			for _, h := range have {
				if h == file {
					return "/bin/" + file, nil
				}
			}
			return "", errors.New("not found")
		})
	}
	tests := []struct {
		name  string
		have  []string
		want  FFmpeg
		found bool
	}{
		{"both", []string{"ffmpeg", "ffprobe"}, FFmpeg{FFmpeg: "/bin/ffmpeg", FFprobe: "/bin/ffprobe"}, true},
		{"only ffmpeg", []string{"ffmpeg"}, FFmpeg{FFmpeg: "/bin/ffmpeg"}, false},
		{"only ffprobe", []string{"ffprobe"}, FFmpeg{FFprobe: "/bin/ffprobe"}, false},
		{"neither", nil, FFmpeg{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newService(t, look(tc.have...))

			got := s.FFmpeg()

			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.found, got.Found())
		})
	}
}
