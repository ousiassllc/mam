package lipsync_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/lipsync"
)

// abs is an absolute path on every OS (the temporary directory is one).
func abs(parts ...string) string {
	return filepath.Join(append([]string{os.TempDir(), "work"}, parts...)...)
}

func goodRequest() lipsync.Request {
	return lipsync.Request{VideoPath: abs("in.mp4"), AudioPath: abs("in.wav"), OutputPath: abs("out.mp4"), FFmpegPath: abs("bin", "ffmpeg")}
}

type fake struct {
	*httptest.Server
	got    lipsync.Request
	token  string
	path   string
	method string
}

// serve answers with status and body, and records what it was sent.
func serve(t *testing.T, status int, body string) *fake {
	t.Helper()
	f := &fake{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.token, f.path, f.method = r.Header.Get(lipsync.TokenHeader), r.URL.Path, r.Method
		if r.Method == http.MethodPost {
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&f.got))
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(f.Close)
	return f
}

func TestLipsyncSendsTheRequestWithTheTokenAndReadsTheReport(t *testing.T) {
	f := serve(t, http.StatusOK, `{"output_path":"/work/out.mp4","frames":30,"fps":25,"duration_sec":1.2,"elapsed_sec":93.6}`)
	c := lipsync.Client{BaseURL: f.URL, Token: "secret-token"}
	req := goodRequest()
	req.FPS = 30
	res, err := c.Lipsync(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, lipsync.Response{OutputPath: "/work/out.mp4", Frames: 30, FPS: 25, DurationSec: 1.2, ElapsedSec: 93.6}, res)
	assert.Equal(t, req, f.got, "the request goes over as it is")
	assert.Equal(t, "secret-token", f.token)
	assert.Equal(t, "/v1/lipsync", f.path)
	assert.Equal(t, http.MethodPost, f.method)
}

// The code of the server's answer is the error: the caller branches with errors.Is, the user reads the sentence.
func TestLipsyncMapsTheServersRefusalsToErrors(t *testing.T) {
	for code, tc := range map[lipsync.Code]struct {
		status int
		is     error
	}{
		lipsync.CodeNoFace:        {422, lipsync.ErrNoFace},
		lipsync.CodeOutOfMemory:   {503, lipsync.ErrOutOfMemory},
		lipsync.CodeBusy:          {409, lipsync.ErrBusy},
		lipsync.CodeTooLarge:      {413, lipsync.ErrTooLarge},
		lipsync.CodeFFmpegFailed:  {422, lipsync.ErrFFmpegFailed},
		lipsync.CodeInputNotFound: {404, lipsync.ErrInputNotFound},
		lipsync.CodeBadRequest:    {400, lipsync.ErrBadRequest},
		lipsync.CodeForbidden:     {403, lipsync.ErrForbidden},
		lipsync.CodeInternal:      {500, lipsync.ErrInternal},
	} {
		t.Run(string(code), func(t *testing.T) {
			f := serve(t, tc.status, `{"error":{"code":"`+string(code)+`","message":"文: `+string(code)+`"}}`)
			_, err := lipsync.Client{BaseURL: f.URL, Token: "t"}.Lipsync(context.Background(), goodRequest())
			require.ErrorIs(t, err, tc.is)
			var le *lipsync.Error
			require.ErrorAs(t, err, &le)
			assert.Equal(t, code, le.Code)
			assert.Equal(t, tc.status, le.Status)
			assert.Equal(t, "文: "+string(code), err.Error(), "the message is the server's sentence, as it is")
			for _, other := range []error{lipsync.ErrNoFace, lipsync.ErrOutOfMemory, lipsync.ErrBusy} {
				if !errors.Is(tc.is, other) {
					assert.NotErrorIs(t, err, other)
				}
			}
		})
	}
}

func TestLipsyncTreatsAnUnknownCodeOrAForeignBodyAsAnInternalError(t *testing.T) {
	for name, body := range map[string]string{
		"an unknown code":  `{"error":{"code":"brand_new","message":"新しい"}}`,
		"not json":         "<html>502 Bad Gateway</html>",
		"an empty message": `{"error":{"code":"no_face","message":""}}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := serve(t, 502, body)
			_, err := lipsync.Client{BaseURL: f.URL}.Lipsync(context.Background(), goodRequest())
			require.ErrorIs(t, err, lipsync.ErrInternal)
			var le *lipsync.Error
			require.ErrorAs(t, err, &le)
			assert.Equal(t, 502, le.Status)
		})
	}
	f := serve(t, 502, "<html>502 Bad Gateway</html>")
	_, err := lipsync.Client{BaseURL: f.URL}.Lipsync(context.Background(), goodRequest())
	assert.Contains(t, err.Error(), "HTTP 502")
	assert.Contains(t, err.Error(), "Bad Gateway", "what the server said is in the message")
}

func TestLipsyncRefusesAnAnswerThatIsNotAReport(t *testing.T) {
	f := serve(t, 200, `{"frames": 3}`)
	_, err := lipsync.Client{BaseURL: f.URL}.Lipsync(context.Background(), goodRequest())
	require.ErrorIs(t, err, lipsync.ErrInternal)
}

func TestLipsyncReportsAServerThatDoesNotAnswer(t *testing.T) {
	f := serve(t, 200, `{}`)
	url := f.URL
	f.Close()
	_, err := lipsync.Client{BaseURL: url}.Lipsync(context.Background(), goodRequest())
	require.ErrorIs(t, err, lipsync.ErrUnreachable)
	assert.NotContains(t, err.Error(), url, "the address is not in the message")
	require.ErrorIs(t, lipsync.Client{BaseURL: url}.Health(context.Background()), lipsync.ErrUnreachable)
}

func TestLipsyncStopsWhenTheContextEnds(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := lipsync.Client{BaseURL: srv.URL}.Lipsync(ctx, goodRequest())
		done <- err
	}()
	cancel()
	require.ErrorIs(t, <-done, context.Canceled, "a cancelled call is the cancel, not an unreachable server")
}

func TestCheckRefusesWhatTheServerWouldRefuse(t *testing.T) {
	for name, edit := range map[string]func(*lipsync.Request){
		"a relative video":          func(r *lipsync.Request) { r.VideoPath = "in.mp4" },
		"no audio":                  func(r *lipsync.Request) { r.AudioPath = "" },
		"no ffmpeg":                 func(r *lipsync.Request) { r.FFmpegPath = "" },
		"an output that is not mp4": func(r *lipsync.Request) { r.OutputPath = abs("out.mov") },
		"the output is the video":   func(r *lipsync.Request) { r.OutputPath = r.VideoPath },
		"the output is the audio":   func(r *lipsync.Request) { r.OutputPath = abs("..", "work", "in.wav") },
		"fps too high":              func(r *lipsync.Request) { r.FPS = 61 },
		"a negative fps":            func(r *lipsync.Request) { r.FPS = -1 },
		"a NUL in a path":           func(r *lipsync.Request) { r.VideoPath += "\x00" },
	} {
		t.Run(name, func(t *testing.T) {
			r := goodRequest()
			edit(&r)
			require.ErrorIs(t, r.Check(), lipsync.ErrBadRequest)
			f := serve(t, 200, `{}`)
			_, err := lipsync.Client{BaseURL: f.URL}.Lipsync(context.Background(), r)
			require.ErrorIs(t, err, lipsync.ErrBadRequest)
			assert.Empty(t, f.path, "nothing was sent")
		})
	}
	require.NoError(t, goodRequest().Check())
	r := goodRequest()
	r.OutputPath = abs("out.MP4")
	require.NoError(t, r.Check(), "the extension is not case sensitive")
}

func TestHealth(t *testing.T) {
	f := serve(t, 200, `{"status":"ok"}`)
	require.NoError(t, lipsync.Client{BaseURL: f.URL, Token: "t"}.Health(context.Background()))
	assert.Equal(t, "/health", f.path)
	assert.Equal(t, http.MethodGet, f.method)

	f = serve(t, 403, `{"error":{"code":"forbidden","message":"このアドレスでは受け付けません"}}`)
	require.ErrorIs(t, lipsync.Client{BaseURL: f.URL}.Health(context.Background()), lipsync.ErrForbidden)
}
