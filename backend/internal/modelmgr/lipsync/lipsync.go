// Package lipsync is the entry of the local lip-sync runtime (MuseTalk 1.5): the request, the response and the errors of one lip-sync
// generation, and the client that sends it to the runtime's server on 127.0.0.1 (spec/architecture/model-manager/local-runtime.md §13.3).
// The Audio Director's local lip-sync provider calls it through localruntime.Service.LipSync, which starts the runtime first. The
// server is mam's own wrapper (catalog/data/overlays/musetalk_server.py); the Python side of the contract is tested with it.
package lipsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/httpclient"
)

// Path is the endpoint of a generation and HealthPath the one that answers once the models are loaded.
const (
	Path       = "/v1/lipsync"
	HealthPath = "/health"
	// TokenHeader carries the token of this start of the server (the server's MAM_RUNTIME_TOKEN).
	TokenHeader = "X-Mam-Token" //nolint:gosec // the name of a header, not a credential
)

// maxResponseBytes bounds what is read of an answer (a small JSON).
const maxResponseBytes = 1 << 20

// Request is one generation: the video (or one image) the face is in, the speech, where the mp4 goes and the ffmpeg to use. Every
// path is absolute and on this machine (the server reads and writes them); the runtime never gets bytes over the wire, so a video
// of any size costs nothing to hand over.
type Request struct {
	// VideoPath is a video (mp4, mov, avi, mkv, flv) or an image (jpg, png, bmp, tif) with one face; MuseTalk makes the mouth follow AudioPath.
	VideoPath string `json:"video_path"`
	// AudioPath is the speech: any audio file ffmpeg reads.
	AudioPath string `json:"audio_path"`
	// OutputPath is the mp4 that is written (H.264 with the speech as AAC). It must not be an input.
	OutputPath string `json:"output_path"`
	// FFmpegPath is the ffmpeg that mam found (spec/architecture/ffmpeg-lookup.md): the runtime ships none.
	FFmpegPath string `json:"ffmpeg_path"`
	// FPS is the frame rate of an image input (a video keeps its own). 0 means 25, the rate MuseTalk was trained at.
	FPS int `json:"fps,omitempty"`
}

// Response is a finished generation.
type Response struct {
	OutputPath  string  `json:"output_path"`
	Frames      int     `json:"frames"`
	FPS         int     `json:"fps"`
	DurationSec float64 `json:"duration_sec"`
	// ElapsedSec is how long the generation took.
	ElapsedSec float64 `json:"elapsed_sec"`
}

// Code is the machine-readable reason of a refusal: the server's own codes.
type Code string

// The codes of the server.
const (
	CodeBadRequest    Code = "bad_request"     // a field is missing or wrong, or the input cannot be read as a video
	CodeForbidden     Code = "forbidden"       // the token or the host is wrong
	CodeInputNotFound Code = "input_not_found" // the video, the audio or ffmpeg is not there
	CodeBusy          Code = "busy"            // another generation is running
	CodeTooLarge      Code = "too_large"       // the video would not fit in memory
	CodeNoFace        Code = "no_face"         // no face was found in the video
	CodeFFmpegFailed  Code = "ffmpeg_failed"   // ffmpeg could not read the input or write the output
	CodeOutOfMemory   Code = "out_of_memory"   // the GPU ran out of video memory
	CodeInternal      Code = "internal"        // anything else the generation failed with
)

// The errors of a generation, to test with errors.Is. *Error carries the code and the sentence for the user.
var (
	ErrBadRequest    = errors.New("lipsync: bad request")
	ErrInputNotFound = errors.New("lipsync: input not found")
	ErrBusy          = errors.New("lipsync: the runtime is generating")
	ErrTooLarge      = errors.New("lipsync: the video is too large")
	ErrNoFace        = errors.New("lipsync: no face in the video")
	ErrFFmpegFailed  = errors.New("lipsync: ffmpeg failed")
	ErrOutOfMemory   = errors.New("lipsync: out of video memory")
	ErrInternal      = errors.New("lipsync: the generation failed")
	// ErrUnreachable: the runtime's server did not answer (it crashed or was stopped during the call).
	ErrUnreachable = errors.New("lipsync: the runtime does not answer")
	// ErrForbidden: the server refused the token (the runtime was restarted since the token was read).
	ErrForbidden = errors.New("lipsync: the runtime refused the request")
)

var byCode = map[Code]error{
	CodeBadRequest: ErrBadRequest, CodeInputNotFound: ErrInputNotFound, CodeBusy: ErrBusy, CodeTooLarge: ErrTooLarge, CodeNoFace: ErrNoFace,
	CodeFFmpegFailed: ErrFFmpegFailed, CodeOutOfMemory: ErrOutOfMemory, CodeInternal: ErrInternal, CodeForbidden: ErrForbidden,
}

// Error is the refusal of the server: Message is its sentence for the user (Japanese), Status the HTTP status.
type Error struct {
	Code    Code
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

// Is makes errors.Is(err, ErrNoFace) and the others true for the error of that code (an unknown code is ErrInternal).
func (e *Error) Is(target error) bool {
	if want, ok := byCode[e.Code]; ok {
		return target == want
	}
	return target == ErrInternal
}

// Check says whether the request can be sent: the same rules the server applies, so that a mistake is caught before the runtime is
// started. It does not touch the disk.
func (r Request) Check() error {
	for name, v := range map[string]string{"video_path": r.VideoPath, "audio_path": r.AudioPath, "output_path": r.OutputPath, "ffmpeg_path": r.FFmpegPath} {
		if v == "" || !filepath.IsAbs(v) || strings.ContainsRune(v, 0) {
			return fmt.Errorf("%w: %s は絶対パスで指定してください", ErrBadRequest, name)
		}
	}
	if r.FPS < 0 || r.FPS > 60 {
		return fmt.Errorf("%w: fps は 1 から 60 で指定してください", ErrBadRequest)
	}
	if !strings.EqualFold(filepath.Ext(r.OutputPath), ".mp4") {
		return fmt.Errorf("%w: output_path は .mp4 のファイルにしてください", ErrBadRequest)
	}
	if filepath.Clean(r.OutputPath) == filepath.Clean(r.VideoPath) || filepath.Clean(r.OutputPath) == filepath.Clean(r.AudioPath) {
		return fmt.Errorf("%w: output_path は入力のファイルと別にしてください", ErrBadRequest)
	}
	return nil
}

// Client sends a Request to the server at BaseURL ("http://127.0.0.1:<port>").
type Client struct {
	BaseURL string
	Token   string
	// HTTP sends the request (nil: a client with no timeout, since a generation takes minutes; ctx ends it).
	HTTP httpclient.Doer
}

func (c Client) do(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rd)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set(TokenHeader, c.Token)
	doer := c.HTTP
	if doer == nil {
		doer = &http.Client{Transport: &http.Transport{Proxy: nil}} // the server is on this machine: never through a proxy
	}
	res, err := doer.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, ctx.Err()
		}
		return 0, nil, fmt.Errorf("%w: %w", ErrUnreachable, httpclient.RedactURL(err))
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, ctx.Err()
		}
		return 0, nil, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	return res.StatusCode, data, nil
}

// Lipsync generates the video and returns what the server reports. A refusal of the server is an *Error; a server that does not
// answer is ErrUnreachable; ctx ends the wait (the server goes on with the generation it started).
func (c Client) Lipsync(ctx context.Context, r Request) (Response, error) {
	if err := r.Check(); err != nil {
		return Response{}, err
	}
	body, err := json.Marshal(r)
	if err != nil {
		return Response{}, err
	}
	status, data, err := c.do(ctx, http.MethodPost, Path, body)
	if err != nil {
		return Response{}, err
	}
	if status != http.StatusOK {
		return Response{}, parseError(status, data)
	}
	var out Response
	if err := json.Unmarshal(data, &out); err != nil || out.OutputPath == "" {
		return Response{}, &Error{Code: CodeInternal, Status: status, Message: "ローカルの口パクのサーバーの応答を読めませんでした: " + httpclient.Snippet(data)}
	}
	return out, nil
}

// Health asks whether the server answers (the models are loaded). It is the readiness probe of the runtime.
func (c Client) Health(ctx context.Context) error {
	status, data, err := c.do(ctx, http.MethodGet, HealthPath, nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return parseError(status, data)
	}
	return nil
}

// parseError reads the {"error": {"code", "message"}} of the server; anything else is an internal error with what was said.
func parseError(status int, data []byte) error {
	var body struct {
		Error struct {
			Code    Code   `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &body) == nil && body.Error.Code != "" && body.Error.Message != "" {
		return &Error{Code: body.Error.Code, Status: status, Message: body.Error.Message}
	}
	return &Error{Code: CodeInternal, Status: status, Message: fmt.Sprintf("ローカルの口パクのサーバーがエラーを返しました（HTTP %d）: %s", status, httpclient.Snippet(data))}
}
