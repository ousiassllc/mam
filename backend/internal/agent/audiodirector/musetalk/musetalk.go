// Package musetalk is the local lip sync of the Audio Director: it makes the mouth of the face in a clip follow the speech with MuseTalk
// 1.5, the runtime that mam installs, starts and stops itself (package localruntime; the entry is package modelmgr/lipsync), in the
// same style as the local voice (package gptsovits) (spec/architecture/model-adapters-m3.md §14.9). It implements
// audiodirector.LocalLipSyncer.
package musetalk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/lipsync"
)

// Limits and defaults of Config.
const (
	// DefaultBaseTimeout, DefaultPerSecondTimeout and DefaultMaxTimeout bound one clip: the base is for the waiting and the loading
	// around a generation, and each second of video adds the per-second part (a GPU is far faster than the 95 s a CPU took for a
	// 1.2 s clip, and a 4 GB GPU far slower than a big one, so the limit follows the length of the clip, not a flat number). It is the
	// backstop against a server that took the request and never answers.
	DefaultBaseTimeout      = 3 * time.Minute
	DefaultPerSecondTimeout = 30 * time.Second
	DefaultMaxTimeout       = 30 * time.Minute
	// DefaultMaxVideoBytes, DefaultMaxAudioBytes and DefaultMaxOutputBytes bound the clip handed over, the speech handed over and the
	// video made (the output is the same limit as the Sync Labs one).
	DefaultMaxVideoBytes  = 512 << 20
	DefaultMaxAudioBytes  = 128 << 20
	DefaultMaxOutputBytes = 512 << 20
)

// Runner is the MuseTalk runtime (*locallipsync.Runner): what it fails with is an *localvoice.UnavailableError, except the refusals
// of the server (*lipsync.Error, lipsync.ErrUnreachable), which the Client reads.
type Runner interface {
	// Prepare starts the runtime when it is stopped and waits until it answers.
	Prepare(ctx context.Context) error
	LipSync(ctx context.Context, req lipsync.Request) (lipsync.Response, error)
}

// Config is the setting of the Client; zero fields take the defaults.
type Config struct {
	// FFmpegPath is the ffmpeg the runtime runs (the runtime ships none): the one mam found (spec/architecture/ffmpeg-lookup.md).
	FFmpegPath string
	// WorkDir is where the clip and the speech are put for the runtime to read ("" is the temporary directory).
	WorkDir string
	// BaseTimeout, PerSecondTimeout and MaxTimeout bound one clip: BaseTimeout + PerSecondTimeout per second of video, at most MaxTimeout.
	BaseTimeout, PerSecondTimeout, MaxTimeout time.Duration
	// MaxVideoBytes, MaxAudioBytes and MaxOutputBytes bound the clip handed over, the speech handed over and the video made.
	MaxVideoBytes, MaxAudioBytes, MaxOutputBytes int
}

// Client makes lip-synced clips with the MuseTalk runtime. The runtime makes one clip at a time.
type Client struct {
	cfg    Config
	runner Runner
}

// New returns a Client of the runtime.
func New(runner Runner, cfg Config) *Client {
	if cfg.BaseTimeout <= 0 {
		cfg.BaseTimeout = DefaultBaseTimeout
	}
	if cfg.PerSecondTimeout <= 0 {
		cfg.PerSecondTimeout = DefaultPerSecondTimeout
	}
	if cfg.MaxTimeout <= 0 {
		cfg.MaxTimeout = DefaultMaxTimeout
	}
	for _, l := range []struct {
		v *int
		d int
	}{{&cfg.MaxVideoBytes, DefaultMaxVideoBytes}, {&cfg.MaxAudioBytes, DefaultMaxAudioBytes}, {&cfg.MaxOutputBytes, DefaultMaxOutputBytes}} {
		if *l.v <= 0 {
			*l.v = l.d
		}
	}
	return &Client{cfg: cfg, runner: runner}
}

// Prepare starts the runtime when it is stopped and waits until it answers (audiodirector.LocalLipSyncer). A runtime that cannot be
// used is an *localvoice.UnavailableError.
func (c *Client) Prepare(ctx context.Context) error { return c.runner.Prepare(ctx) }

// timeout is the limit of a clip of the given length.
func (c *Client) timeout(seconds float64) time.Duration {
	return min(c.cfg.BaseTimeout+time.Duration(max(seconds, 0)*float64(c.cfg.PerSecondTimeout)), c.cfg.MaxTimeout)
}

// Sync puts the clip and the speech where the runtime can read them, has it make the mouth follow the speech and returns the mp4
// (audiodirector.LipSyncer). req.Model is not used: MuseTalk 1.5 is the only model. A failure is an *localvoice.UnavailableError (no face,
// out of video memory, too large, not installed, did not answer, over the time limit, an empty answer); the caller's own cancellation
// is ctx's error. Nothing is charged, so no failure is an audiodirector.ChargedError.
//
// The clip is handed over as the Director made it. MuseTalk needs no conforming by mam: the server reads the frame rate of the video
// itself (every frame is a frame of the picture, and the speech is cut at that rate), turns the speech into 16 kHz mono itself, and any
// picture size is accepted up to what fits in memory (a video that does not is KindTooLarge, with a sentence to shorten it).
func (c *Client) Sync(ctx context.Context, req audiodirector.LipSyncRequest) ([]byte, error) {
	switch {
	case len(req.Video.Data) == 0 || len(req.Audio.Data) == 0:
		return nil, errors.New("musetalk: the clip or the speech is empty")
	case len(req.Video.Data) > c.cfg.MaxVideoBytes:
		return nil, c.unavailable(localvoice.KindTooLarge, fmt.Sprintf("動画が %d MB を超えています", c.cfg.MaxVideoBytes>>20), nil)
	case len(req.Audio.Data) > c.cfg.MaxAudioBytes:
		return nil, c.unavailable(localvoice.KindTooLarge, fmt.Sprintf("音声が %d MB を超えています", c.cfg.MaxAudioBytes>>20), nil)
	case c.cfg.FFmpegPath == "":
		return nil, errors.New("musetalk: the path of ffmpeg is not set")
	}
	ffmpeg, err := filepath.Abs(c.cfg.FFmpegPath)
	if err != nil {
		return nil, fmt.Errorf("musetalk: ffmpeg path: %w", err)
	}
	dir, err := os.MkdirTemp(c.cfg.WorkDir, "mam-musetalk-")
	if err != nil {
		return nil, fmt.Errorf("musetalk: create work directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	video, speech, out := filepath.Join(dir, "clip.mp4"), filepath.Join(dir, "voice.wav"), filepath.Join(dir, "lipsync.mp4")
	if err := fsutil.WriteFileAtomic(video, req.Video.Data, 0o600); err != nil {
		return nil, fmt.Errorf("musetalk: write clip: %w", err)
	}
	if err := fsutil.WriteFileAtomic(speech, req.Audio.Data, 0o600); err != nil {
		return nil, fmt.Errorf("musetalk: write speech: %w", err)
	}

	limit := c.timeout(req.Seconds)
	callCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	_, err = c.runner.LipSync(callCtx, lipsync.Request{VideoPath: video, AudioPath: speech, OutputPath: out, FFmpegPath: ffmpeg})
	if err != nil {
		return nil, c.failure(ctx, callCtx, limit, err)
	}
	return c.readOutput(out)
}

// failure is the typed error of a failed call: the caller's own cancellation stays as it is, the clip limit is a timeout, and the
// server's refusals are told apart by their code, as the user can do something about no face, a long clip or a small GPU.
func (c *Client) failure(ctx, callCtx context.Context, limit time.Duration, err error) error {
	var (
		un      *localvoice.UnavailableError
		refused *lipsync.Error
	)
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case callCtx.Err() != nil:
		return c.unavailable(localvoice.KindTimeout, fmt.Sprintf("1 クリップの口パクが %s で終わりませんでした", limit), err)
	case errors.As(err, &un):
		return err
	case errors.As(err, &refused):
		return c.unavailable(kindOf(refused.Code), refused.Message, err)
	}
	return c.unavailable(localvoice.KindProcess, err.Error(), err)
}

// kindOf is the Kind of a code of the server (the codes that are no fact of the clip or the GPU are a failure of the process).
func kindOf(code lipsync.Code) localvoice.Kind {
	switch code {
	case lipsync.CodeNoFace:
		return localvoice.KindNoFace
	case lipsync.CodeOutOfMemory:
		return localvoice.KindGPUMemory
	case lipsync.CodeTooLarge:
		return localvoice.KindTooLarge
	}
	return localvoice.KindProcess
}

func (c *Client) unavailable(kind localvoice.Kind, detail string, err error) error {
	return &localvoice.UnavailableError{Kind: kind, Vendor: localvoice.VendorMuseTalk, Detail: detail, Err: err}
}

// readOutput reads the mp4 the runtime made: an empty file, or one over the limit, is a failure.
func (c *Client) readOutput(path string) ([]byte, error) {
	f, err := os.Open(path) // #nosec G304 -- the file this call had the runtime write
	if err != nil {
		return nil, c.unavailable(localvoice.KindProcess, "口パクの動画ができていません", err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, int64(c.cfg.MaxOutputBytes)+1))
	switch {
	case err != nil:
		return nil, fmt.Errorf("musetalk: read output: %w", err)
	case len(data) == 0:
		return nil, c.unavailable(localvoice.KindProcess, "口パクの動画が空でした", nil)
	case len(data) > c.cfg.MaxOutputBytes:
		return nil, c.unavailable(localvoice.KindTooLarge, fmt.Sprintf("口パクの動画が %d MB を超えました", c.cfg.MaxOutputBytes>>20), nil)
	}
	return data, nil
}
