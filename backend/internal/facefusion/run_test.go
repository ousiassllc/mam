package facefusion

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeFaceFusion is a FaceFusion directory (facefusion.py and facefusion/metadata.py) and a "python" that is a shell script standing in for
// FaceFusion's headless-run: it records its arguments and working directory into log files and runs body, which sees $target and $out.
type fakeFaceFusion struct {
	Install
	argsLog, cwdLog string
}

func newFake(t *testing.T, body string) fakeFaceFusion {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake python is a shell script")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "facefusion-3.9.2")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "facefusion"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, Script), []byte("# fake\n"), 0o600))
	meta := "METADATA =\\\n{\n\t'name': 'FaceFusion',\n\t'version': '3.9.2',\n\t'license': 'OpenRAIL-AS',\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "facefusion", "metadata.py"), []byte(meta), 0o600))
	f := fakeFaceFusion{argsLog: filepath.Join(root, "args.log"), cwdLog: filepath.Join(root, "cwd.log")}
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + f.argsLog + "'\npwd > '" + f.cwdLog + "'\n" +
		"while [ $# -gt 0 ]; do case \"$1\" in --target-path) target=\"$2\";; --output-path) out=\"$2\";; esac; shift; done\n" + body + "\n"
	python := filepath.Join(root, "python")
	require.NoError(t, os.WriteFile(python, []byte(script), 0o700)) // #nosec G306 -- a test executable
	f.Install = Install{Python: python, Dir: dir}
	return f
}

func pngBytes(t *testing.T, w int) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, 1))))
	return buf.Bytes()
}

func lines(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path) // #nosec G304 -- a file of the test's temp directory
	require.NoError(t, err)
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func TestSwapRunsHeadlessRunWithTheDefaultModelsAndReturnsTheFile(t *testing.T) {
	f := newFake(t, `cp "$target" "$out"`)
	target, face := pngBytes(t, 3), pngBytes(t, 5)

	got, mime, err := f.Swap(t.Context(), Request{Face: face, Target: target, FaceExt: ".png", TargetExt: ".png"})

	require.NoError(t, err)
	assert.Equal(t, "image/png", mime)
	assert.Equal(t, target, got, "the fake copies the target into the output")
	args := lines(t, f.argsLog)
	assert.Equal(t, []string{Script, "headless-run"}, args[:2], "facefusion.py is relative to the working directory")
	joined := strings.Join(args, " ")
	assert.Contains(t, joined, "--processors face_swapper face_enhancer")
	assert.Contains(t, joined, "--face-swapper-model ghost_2_256")
	assert.Contains(t, joined, "--face-detector-model yunet")
	assert.Contains(t, joined, "--face-enhancer-model gfpgan_1.4")
	assert.Equal(t, f.Dir, lines(t, f.cwdLog)[0], "the run starts in the FaceFusion directory")
	srcAt := indexAfter(args, "--source-paths")
	assert.True(t, strings.HasSuffix(args[srcAt], "face.png"))
	assert.NoFileExists(t, args[srcAt], "the temporary directory is removed")
}

func indexAfter(args []string, flag string) int {
	for i, a := range args {
		if a == flag {
			return i + 1
		}
	}
	return -1
}

func TestSwapWithoutAnEnhancerRunsOnlyTheSwapper(t *testing.T) {
	f := newFake(t, `cp "$target" "$out"`)

	_, _, err := f.Swap(t.Context(), Request{Face: pngBytes(t, 1), Target: pngBytes(t, 2), FaceExt: ".png", TargetExt: ".png",
		Selection: Selection{Swapper: "ghost_3_256", Enhancer: EnhancerNone}})

	require.NoError(t, err)
	joined := strings.Join(lines(t, f.argsLog), " ")
	assert.Contains(t, joined, "--processors face_swapper --face-swapper-model ghost_3_256")
	assert.NotContains(t, joined, "face_enhancer")
	assert.NotContains(t, joined, "--face-enhancer-model")
}

func TestSwapRefusesAnUnknownModelWithoutStartingAnything(t *testing.T) {
	f := newFake(t, `cp "$target" "$out"`)

	_, _, err := f.Swap(t.Context(), Request{Face: pngBytes(t, 1), Target: pngBytes(t, 2), FaceExt: ".png", TargetExt: ".png",
		Selection: Selection{Swapper: "not_a_model"}})

	require.ErrorContains(t, err, "not_a_model")
	assert.NoFileExists(t, f.argsLog, "FaceFusion was not started")
}

func TestSwapFailureCarriesTheEndOfTheOutput(t *testing.T) {
	f := newFake(t, "echo 'loading' >&2\necho 'Face not detected in source' >&2\nexit 1")

	_, _, err := f.Swap(t.Context(), Request{Face: pngBytes(t, 1), Target: pngBytes(t, 2), FaceExt: ".png", TargetExt: ".png"})

	var re *RunError
	require.ErrorAs(t, err, &re)
	assert.Contains(t, re.Output, "Face not detected in source")
	assert.Contains(t, err.Error(), "FaceFusion が失敗しました")
}

func TestSwapThatEndsWellWithoutAFileIsAnError(t *testing.T) {
	f := newFake(t, "echo 'nothing to do'")

	_, _, err := f.Swap(t.Context(), Request{Face: pngBytes(t, 1), Target: pngBytes(t, 2), FaceExt: ".png", TargetExt: ".png"})

	var re *RunError
	require.ErrorAs(t, err, &re)
	assert.NoError(t, re.Err)
	assert.Contains(t, err.Error(), "出力の画像がありません")
}

func TestSwapThatWritesSomethingThatIsNotAPictureIsAnError(t *testing.T) {
	f := newFake(t, `echo "plain text" > "$out"`)

	_, _, err := f.Swap(t.Context(), Request{Face: pngBytes(t, 1), Target: pngBytes(t, 2), FaceExt: ".png", TargetExt: ".png"})

	require.ErrorContains(t, err, "not a picture")
}

func TestSwapIsKilledAtItsLimit(t *testing.T) {
	f := newFake(t, "exec sleep 60")
	started := time.Now()

	_, _, err := f.Swap(t.Context(), Request{Face: pngBytes(t, 1), Target: pngBytes(t, 2), FaceExt: ".png", TargetExt: ".png", Limit: 200 * time.Millisecond})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Contains(t, err.Error(), "時間内に終わりませんでした")
	assert.Less(t, time.Since(started), 20*time.Second, "the hung process is killed at its limit")
}

func TestSwapStopsWhenTheJobIsCancelled(t *testing.T) {
	f := newFake(t, "exec sleep 60")
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(200*time.Millisecond, cancel)

	_, _, err := f.Swap(ctx, Request{Face: pngBytes(t, 1), Target: pngBytes(t, 2), FaceExt: ".png", TargetExt: ".png"})

	require.ErrorIs(t, err, context.Canceled)
	var re *RunError
	assert.NotErrorAs(t, err, &re, "a cancelled job is not a failure of FaceFusion")
}

func TestTailKeepsTheLastLinesAndTreatsCarriageReturnsAsLineEnds(t *testing.T) {
	var out tail
	_, _ = out.Write([]byte("a\nb\r 10%\r 50%\r100%\n"))
	assert.Equal(t, "a / b / 10% / 50% / 100%", out.Text())

	var long tail
	for range 100 {
		_, _ = long.Write([]byte(strings.Repeat("x", 500) + "\n"))
	}
	_, _ = long.Write([]byte("last line\n"))
	assert.LessOrEqual(t, len(long.buf), 2*maxOutputBytes)
	assert.True(t, strings.HasSuffix(long.Text(), "last line"))
}
