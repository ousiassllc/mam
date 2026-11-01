package musetalk_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/musetalk"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/lipsync"
)

// ffmpeg is an absolute path on every OS (the temporary directory is one); the fake runtime never runs it.
var ffmpeg = filepath.Join(os.TempDir(), "bin", "ffmpeg")

// call is what the fake runtime was asked, with the content of the files it was told to read (they are gone after the call).
type call struct {
	req             lipsync.Request
	video, speech   []byte
	remaining       time.Duration // until the deadline of the call
	hadDeadline     bool
	outputBeforeRun bool
}

// answerFunc decides what the n-th (0-based) call of the fake runtime does: it may write the output file, and returns the error.
type answerFunc func(ctx context.Context, n int, req lipsync.Request) (lipsync.Response, error)

// fakeRuntime is the MuseTalk runtime: it records what it is asked and answers as the test says.
type fakeRuntime struct {
	mu       sync.Mutex
	prepared int
	prepare  error
	calls    []call
	answer   answerFunc
}

func (f *fakeRuntime) Prepare(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prepared++
	return f.prepare
}

func (f *fakeRuntime) LipSync(ctx context.Context, req lipsync.Request) (lipsync.Response, error) {
	f.mu.Lock()
	n := len(f.calls)
	c := call{req: req}
	c.video, _ = os.ReadFile(req.VideoPath)
	c.speech, _ = os.ReadFile(req.AudioPath)
	_, statErr := os.Stat(req.OutputPath)
	c.outputBeforeRun = statErr == nil
	if dl, ok := ctx.Deadline(); ok {
		c.hadDeadline, c.remaining = true, time.Until(dl)
	}
	f.calls = append(f.calls, c)
	answer := f.answer
	f.mu.Unlock()
	if answer == nil {
		return lipsync.Response{}, errors.New("no answer")
	}
	return answer(ctx, n, req)
}

// writes answers a call by writing the output file.
func writes(data []byte) answerFunc {
	return func(_ context.Context, _ int, req lipsync.Request) (lipsync.Response, error) {
		return lipsync.Response{OutputPath: req.OutputPath}, os.WriteFile(req.OutputPath, data, 0o600)
	}
}

// fails answers a call with an error.
func fails(err error) answerFunc {
	return func(context.Context, int, lipsync.Request) (lipsync.Response, error) { return lipsync.Response{}, err }
}

func request() audiodirector.LipSyncRequest {
	return audiodirector.LipSyncRequest{
		Seconds: 4, Video: audiodirector.Sample{Filename: "clip.mp4", Data: []byte("video")}, Audio: audiodirector.Sample{Filename: "voice.wav", Data: []byte("speech")},
	}
}

func unavailable(t *testing.T, err error) *localvoice.UnavailableError {
	t.Helper()
	var un *localvoice.UnavailableError
	require.ErrorAs(t, err, &un)
	assert.Equal(t, localvoice.VendorMuseTalk, un.Vendor)
	return un
}

func TestSync_HandsTheClipAndTheSpeechOverAsFilesAndReturnsTheVideo(t *testing.T) {
	rt := &fakeRuntime{answer: writes([]byte("lip-synced mp4"))}
	c := musetalk.New(rt, musetalk.Config{FFmpegPath: ffmpeg, WorkDir: t.TempDir()})

	data, err := c.Sync(context.Background(), request())

	require.NoError(t, err)
	assert.Equal(t, []byte("lip-synced mp4"), data)
	require.Len(t, rt.calls, 1)
	got := rt.calls[0]
	assert.Equal(t, []byte("video"), got.video, "the runtime reads the clip from the file it was told")
	assert.Equal(t, []byte("speech"), got.speech)
	assert.False(t, got.outputBeforeRun)
	assert.Equal(t, ffmpeg, got.req.FFmpegPath)
	for _, p := range []string{got.req.VideoPath, got.req.AudioPath, got.req.OutputPath} {
		assert.True(t, filepath.IsAbs(p), p)
	}
	require.NoError(t, got.req.Check(), "the request is one the runtime's entry accepts")
	assert.Zero(t, got.req.FPS, "a video keeps its own frame rate")
	_, statErr := os.Stat(filepath.Dir(got.req.VideoPath))
	assert.ErrorIs(t, statErr, os.ErrNotExist, "the work directory is removed")
}

func TestSync_AMissingInputIsRefusedBeforeTheRuntimeIsAsked(t *testing.T) {
	for name, edit := range map[string]func(*audiodirector.LipSyncRequest){
		"an empty clip":   func(r *audiodirector.LipSyncRequest) { r.Video.Data = nil },
		"an empty speech": func(r *audiodirector.LipSyncRequest) { r.Audio.Data = nil },
	} {
		t.Run(name, func(t *testing.T) {
			rt := &fakeRuntime{answer: writes([]byte("x"))}
			req := request()
			edit(&req)
			_, err := musetalk.New(rt, musetalk.Config{FFmpegPath: ffmpeg}).Sync(context.Background(), req)
			require.Error(t, err)
			assert.Empty(t, rt.calls)
		})
	}
	rt := &fakeRuntime{answer: writes([]byte("x"))}
	_, err := musetalk.New(rt, musetalk.Config{}).Sync(context.Background(), request())
	require.Error(t, err, "without the path of ffmpeg the runtime cannot cut the video")
	assert.Empty(t, rt.calls)
}

func TestSync_InputsAndOutputOverTheirLimitsAreTooLarge(t *testing.T) {
	rt := &fakeRuntime{answer: writes([]byte("0123456789"))}
	cfg := musetalk.Config{FFmpegPath: ffmpeg, MaxVideoBytes: 8, MaxAudioBytes: 8, MaxOutputBytes: 8}
	c := musetalk.New(rt, cfg)

	big := request()
	big.Video.Data = []byte("0123456789")
	_, err := c.Sync(context.Background(), big)
	assert.Equal(t, localvoice.KindTooLarge, unavailable(t, err).Kind)

	big = request()
	big.Audio.Data = []byte("0123456789")
	_, err = c.Sync(context.Background(), big)
	assert.Equal(t, localvoice.KindTooLarge, unavailable(t, err).Kind)
	assert.Empty(t, rt.calls, "an input over its limit never reaches the runtime")

	small := request()
	small.Video.Data, small.Audio.Data = []byte("v"), []byte("a")
	_, err = c.Sync(context.Background(), small)
	assert.Equal(t, localvoice.KindTooLarge, unavailable(t, err).Kind, "the video made is over its limit")
}

func TestSync_AnEmptyOrMissingOutputIsAFailure(t *testing.T) {
	for name, answer := range map[string]answerFunc{
		"an empty file": writes(nil),
		"no file":       func(context.Context, int, lipsync.Request) (lipsync.Response, error) { return lipsync.Response{}, nil },
	} {
		t.Run(name, func(t *testing.T) {
			rt := &fakeRuntime{answer: answer}
			_, err := musetalk.New(rt, musetalk.Config{FFmpegPath: ffmpeg}).Sync(context.Background(), request())
			assert.Equal(t, localvoice.KindProcess, unavailable(t, err).Kind)
		})
	}
}

// The code of the server is the Kind: a screen branches on it, the Detail is the server's sentence for the user.
func TestSync_TheServersRefusalsAreToldApart(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		kind localvoice.Kind
	}{
		"no face":         {&lipsync.Error{Code: lipsync.CodeNoFace, Status: 422, Message: "顔が無い"}, localvoice.KindNoFace},
		"out of memory":   {&lipsync.Error{Code: lipsync.CodeOutOfMemory, Status: 503, Message: "メモリ不足"}, localvoice.KindGPUMemory},
		"too large":       {&lipsync.Error{Code: lipsync.CodeTooLarge, Status: 413, Message: "大きすぎる"}, localvoice.KindTooLarge},
		"internal":        {&lipsync.Error{Code: lipsync.CodeInternal, Status: 500, Message: "失敗"}, localvoice.KindProcess},
		"ffmpeg failed":   {&lipsync.Error{Code: lipsync.CodeFFmpegFailed, Status: 422, Message: "ffmpeg"}, localvoice.KindProcess},
		"unreachable":     {lipsync.ErrUnreachable, localvoice.KindProcess},
		"anything else":   {errors.New("boom"), localvoice.KindProcess},
		"a runtime error": {&localvoice.UnavailableError{Kind: localvoice.KindWeightsMissing, Vendor: localvoice.VendorMuseTalk, Detail: "重みが無い"}, localvoice.KindWeightsMissing},
	} {
		t.Run(name, func(t *testing.T) {
			rt := &fakeRuntime{answer: fails(tc.err)}
			_, err := musetalk.New(rt, musetalk.Config{FFmpegPath: ffmpeg}).Sync(context.Background(), request())
			un := unavailable(t, err)
			assert.Equal(t, tc.kind, un.Kind)
			var refused *lipsync.Error
			if errors.As(tc.err, &refused) {
				assert.Equal(t, refused.Message, un.Detail, "the server's sentence reaches the user as it is")
				assert.ErrorIs(t, err, tc.err, "the refusal stays reachable with errors.Is")
			}
			assert.Equal(t, tc.kind == localvoice.KindWeightsMissing, un.Systemic())
		})
	}
}

func TestSync_ACallThatRunsOverItsLimitIsATimeoutAndTheCallersCancelIsNot(t *testing.T) {
	blocks := func(ctx context.Context, _ int, _ lipsync.Request) (lipsync.Response, error) {
		<-ctx.Done()
		return lipsync.Response{}, ctx.Err()
	}
	rt := &fakeRuntime{answer: blocks}
	c := musetalk.New(rt, musetalk.Config{FFmpegPath: ffmpeg, BaseTimeout: time.Millisecond, PerSecondTimeout: time.Millisecond, MaxTimeout: 5 * time.Millisecond})

	_, err := c.Sync(context.Background(), request())
	assert.Equal(t, localvoice.KindTimeout, unavailable(t, err).Kind)
	assert.False(t, unavailable(t, err).Systemic(), "the next clip may be shorter")

	ctx, cancel := context.WithCancel(context.Background())
	rt = &fakeRuntime{answer: func(ctx context.Context, n int, req lipsync.Request) (lipsync.Response, error) {
		cancel()
		return blocks(ctx, n, req)
	}}
	_, err = musetalk.New(rt, musetalk.Config{FFmpegPath: ffmpeg}).Sync(ctx, request())
	require.ErrorIs(t, err, context.Canceled)
	var un *localvoice.UnavailableError
	assert.False(t, errors.As(err, &un), "the caller's own cancel is no failure of the runtime")
}

// A GPU is far faster or slower than another: the limit of a clip is the base plus a share of each second, up to the maximum.
func TestSync_TheLimitOfAClipFollowsItsLength(t *testing.T) {
	cfg := musetalk.Config{FFmpegPath: ffmpeg, BaseTimeout: 10 * time.Minute, PerSecondTimeout: time.Minute, MaxTimeout: 20 * time.Minute}
	for _, tc := range []struct {
		seconds float64
		want    time.Duration
	}{{0, 10 * time.Minute}, {4, 14 * time.Minute}, {600, 20 * time.Minute}} {
		rt := &fakeRuntime{answer: writes([]byte("x"))}
		req := request()
		req.Seconds = tc.seconds
		_, err := musetalk.New(rt, cfg).Sync(context.Background(), req)
		require.NoError(t, err)
		require.True(t, rt.calls[0].hadDeadline)
		assert.InDelta(t, tc.want.Seconds(), rt.calls[0].remaining.Seconds(), 30, "%g s of video", tc.seconds)
	}

	rt := &fakeRuntime{answer: writes([]byte("x"))}
	_, err := musetalk.New(rt, musetalk.Config{FFmpegPath: ffmpeg}).Sync(context.Background(), request())
	require.NoError(t, err)
	assert.InDelta(t, (musetalk.DefaultBaseTimeout + 4*musetalk.DefaultPerSecondTimeout).Seconds(), rt.calls[0].remaining.Seconds(), 30, "the defaults")
}

func TestPrepare_StartsTheRuntimeAndReturnsItsRefusalAsItIs(t *testing.T) {
	refusal := &localvoice.UnavailableError{Kind: localvoice.KindNotInstalled, Vendor: localvoice.VendorMuseTalk}
	rt := &fakeRuntime{prepare: refusal}
	err := musetalk.New(rt, musetalk.Config{}).Prepare(context.Background())
	assert.Same(t, refusal, unavailable(t, err))
	assert.Equal(t, 1, rt.prepared)
}
