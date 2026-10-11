package testenv

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// madeFile is one ffmpeg result of the process: made once, read by every test that asks for it.
type madeFile struct {
	once sync.Once
	data []byte
	err  string
}

var madeFiles sync.Map // key (extension + arguments) -> *madeFile

// FFmpegFile is a media file that ffmpeg makes from args (the input and codec arguments, without
// "-v", "-y" and the output file): the bytes of "ffmpeg -v error -y <args> out<ext>". The same ext
// and args are encoded once per test process and every test gets the same bytes, so a fixture that
// every test needs (a colour pattern video, a sine tone) costs one ffmpeg run, not one per test.
// The returned slice is shared: do not modify it. ext includes the dot (".mp4").
//
// A machine without ffmpeg ends the test as MissingFFmpeg does.
func FFmpegFile(t testing.TB, ext string, args ...string) []byte {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		MissingFFmpeg(t, "ffmpeg is not installed")
	}
	v, _ := madeFiles.LoadOrStore(ext+"\x00"+strings.Join(args, "\x00"), &madeFile{})
	m := v.(*madeFile)
	m.once.Do(func() { m.data, m.err = encode(ffmpeg, ext, args) })
	if m.err != "" {
		t.Fatalf("ffmpeg %s: %s", strings.Join(args, " "), m.err)
	}
	return m.data
}

// WriteFFmpegFile writes FFmpegFile(t, ext, args...) to path: the file a test may open, move or
// change for itself.
func WriteFFmpegFile(t testing.TB, path, ext string, args ...string) {
	t.Helper()
	data := FFmpegFile(t, ext, args...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func encode(ffmpeg, ext string, args []string) ([]byte, string) {
	dir, err := os.MkdirTemp("", "mam-ffmpeg-")
	if err != nil {
		return nil, err.Error()
	}
	defer func() { _ = os.RemoveAll(dir) }()
	out := dir + string(os.PathSeparator) + "out" + ext
	cmd := exec.Command(ffmpeg, append(append([]string{"-v", "error", "-y"}, args...), out)...) // #nosec G204 -- test fixtures
	if b, err := cmd.CombinedOutput(); err != nil {
		return nil, err.Error() + ": " + string(b)
	}
	data, err := os.ReadFile(out) // #nosec G304 -- a file of the temp directory this function made
	if err != nil {
		return nil, err.Error()
	}
	return data, ""
}

// PatternVideo is a 10 fps mp4 of a colour test pattern (size "320x240") with a 440 Hz tone, seconds
// long: the video the Take fixtures of the HTTP tests hold. Shared like FFmpegFile: do not modify it.
func PatternVideo(t testing.TB, size string, seconds int) []byte {
	t.Helper()
	d := strconv.Itoa(seconds)
	return FFmpegFile(t, ".mp4", "-f", "lavfi", "-i", "testsrc=size="+size+":rate=10:duration="+d,
		"-f", "lavfi", "-i", "sine=frequency=440:duration="+d, "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac")
}
