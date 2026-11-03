package facefusion

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DefaultLimit is how long a run may take before it is killed. The first run of a model is long, since FaceFusion fetches the model
// then (it does that itself; mam does not), so the limit is generous.
const DefaultLimit = 30 * time.Minute

// waitDelay is how long a killed process may keep its output pipe open before it is closed on it.
const waitDelay = 5 * time.Second

// maxOutputBytes bounds what is kept of the output of a run for the error message.
const maxOutputBytes = 4 << 10

// commandContext builds the exec.Cmd of a run. It is a variable so that a test can look at the command.
var commandContext = exec.CommandContext

// Request is one swap: the picture of the face (the source of FaceFusion) and the picture to put it into (the target), as the bytes of
// image files with the extension each has (".png", ".jpg", ".webp"), and the models.
type Request struct {
	Face, Target       []byte
	FaceExt, TargetExt string
	Selection          Selection
	// Limit is the time a run may take (zero: DefaultLimit).
	Limit time.Duration
}

// RunError is a run of FaceFusion that did not make a picture. Output is the end of what it printed.
type RunError struct {
	// Err is why the process ended (an *exec.ExitError for a non-zero exit, a time limit, a Python that could not be started); nil when it
	// ended well and made no file.
	Err    error
	Output string
}

func (e *RunError) Error() string {
	var head string
	switch {
	case errors.Is(e.Err, context.DeadlineExceeded):
		head = "FaceFusion が時間内に終わりませんでした"
	case e.Err != nil:
		head = "FaceFusion が失敗しました（" + e.Err.Error() + "）"
	default:
		head = "FaceFusion は終わりましたが、出力の画像がありません"
	}
	if e.Output == "" {
		return head
	}
	return head + ": " + e.Output
}

func (e *RunError) Unwrap() error { return e.Err }

// Args are the arguments `python` is started with for a swap of the files: FaceFusion's non-interactive mode (`headless-run`), the three
// models, and a private job and temp directory so that nothing is written into the installation. facefusion.py is relative: the run's
// working directory is the FaceFusion directory.
func Args(sel Selection, face, target, output, work string) []string {
	sel = sel.WithDefaults()
	processors := []string{"face_swapper"}
	if sel.Enhancer != EnhancerNone {
		processors = append(processors, "face_enhancer")
	}
	args := []string{
		Script, "headless-run",
		"--source-paths", face, "--target-path", target, "--output-path", output,
		"--jobs-path", filepath.Join(work, "jobs"), "--temp-path", filepath.Join(work, "temp"),
		"--processors",
	}
	args = append(args, processors...)
	args = append(args, "--face-swapper-model", sel.Swapper, "--face-detector-model", sel.Detector)
	if sel.Enhancer != EnhancerNone {
		args = append(args, "--face-enhancer-model", sel.Enhancer)
	}
	return args
}

// Swap puts the face of req.Face into req.Target with FaceFusion and returns the picture it made and its media type. A run that fails, or ends
// without a file, is a *RunError; the context's own end is returned as it is (a cancelled job). The pictures go through a temporary directory
// that is removed afterwards.
func (in Install) Swap(ctx context.Context, req Request) (data []byte, mimeType string, err error) {
	if _, _, _, err := req.Selection.Resolve(); err != nil {
		return nil, "", err
	}
	work, err := os.MkdirTemp("", "mam-facefusion-")
	if err != nil {
		return nil, "", fmt.Errorf("facefusion: temporary directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(work) }()
	face, target, output := filepath.Join(work, "face"+req.FaceExt), filepath.Join(work, "target"+req.TargetExt), filepath.Join(work, "output.png")
	if err := os.WriteFile(face, req.Face, 0o600); err != nil {
		return nil, "", fmt.Errorf("facefusion: write the face: %w", err)
	}
	if err := os.WriteFile(target, req.Target, 0o600); err != nil {
		return nil, "", fmt.Errorf("facefusion: write the target: %w", err)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	runCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	cmd := commandContext(runCtx, in.Python, Args(req.Selection, face, target, output, work)...) // #nosec G204 -- the executable is the one the user set; the arguments are built here from checked model ids and our own temporary files
	cmd.Dir = in.Dir
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8", "PYTHONUTF8=1", "PYTHONUNBUFFERED=1")
	hideWindow(cmd)
	var out tail
	cmd.Stdout, cmd.Stderr = &out, &out
	cmd.WaitDelay = waitDelay
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		if runCtx.Err() != nil {
			err = runCtx.Err()
		}
		return nil, "", &RunError{Err: err, Output: out.Text()}
	}
	data, err = os.ReadFile(output) //nolint:gosec // our own temporary file
	if err != nil || len(data) == 0 {
		return nil, "", &RunError{Output: out.Text()}
	}
	mimeType = http.DetectContentType(data)
	if !strings.HasPrefix(mimeType, "image/") {
		return nil, "", &RunError{Err: fmt.Errorf("the output is %s, not a picture", mimeType), Output: out.Text()}
	}
	return data, mimeType, nil
}

// tail keeps the end of what is written to it (the last maxOutputBytes bytes), for the error message of a failed run.
type tail struct{ buf []byte }

func (t *tail) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > 2*maxOutputBytes {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-maxOutputBytes:]...)
	}
	return len(p), nil
}

// Text is the last lines of the output: progress bars redraw one line with carriage returns, so those are line ends too.
func (t *tail) Text() string {
	b := t.buf
	if len(b) > maxOutputBytes {
		b = b[len(b)-maxOutputBytes:]
	}
	b = bytes.ReplaceAll(b, []byte("\r"), []byte("\n"))
	var lines []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	return strings.Join(lines, " / ")
}
