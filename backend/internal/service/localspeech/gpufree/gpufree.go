// Package gpufree is how the local voice Irodori-TTS makes room on the GPU before its server starts when the bundled ComfyUI is up (the VRAM
// policy of spec/architecture/model-adapters-m3.md §14.12): ComfyUI keeps the models of its last generation in video memory, which on a 16 GB GPU
// leaves too little for the voice. The two are never made to work at the same moment: a ComfyUI that is generating holds the voice back (it
// would run out of memory, and the generation would suffer), and an idle one is asked to let go of its models (POST /free, which is what
// ComfyUI's own "unload models" does). It is a package of its own so that localspeech stays small.
package gpufree

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/httpclient"
)

// Engine is the bundled ComfyUI as the guard needs it: where it listens, and "" while it is not running (a stopped engine holds no video
// memory, so there is nothing to do).
type Engine interface {
	URL(ctx context.Context) string
}

// Guard frees the video memory of the bundled ComfyUI for the voice.
type Guard struct {
	Engine Engine
	// HTTP sends the requests to ComfyUI (nil: a plain client).
	HTTP httpclient.Doer
}

const maxAnswerBytes = 1 << 20

// Before is the hook of localspeech.Server.Before. It does nothing when ComfyUI is not running; it is an *localvoice.UnavailableError of the
// kind GPUMemory when ComfyUI is generating (nothing is freed); otherwise ComfyUI is asked to unload its models and free its memory. A ComfyUI
// that cannot be reached is not a reason to hold the voice back: the voice then starts, and runs out of memory with its own sentence if it
// must.
func (g Guard) Before(ctx context.Context) error {
	if g.Engine == nil {
		return nil
	}
	base := strings.TrimRight(g.Engine.URL(ctx), "/")
	if base == "" {
		return nil
	}
	busy, err := g.busy(ctx, base)
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case err != nil:
		return nil
	case busy:
		return &localvoice.UnavailableError{Kind: localvoice.KindGPUMemory, Vendor: localvoice.VendorIrodori,
			Detail: "同梱の ComfyUI が画像・動画を生成している最中です。生成が終わってから、もう一度実行してください"}
	}
	_ = g.post(ctx, base+"/free", []byte(`{"unload_models":true,"free_memory":true}`))
	return nil
}

// busy reports whether ComfyUI has a generation running or waiting (GET /queue).
func (g Guard) busy(ctx context.Context, base string) (bool, error) {
	data, err := g.do(ctx, http.MethodGet, base+"/queue", nil)
	if err != nil {
		return false, err
	}
	var q struct {
		Running []json.RawMessage `json:"queue_running"`
		Pending []json.RawMessage `json:"queue_pending"`
	}
	if err := json.Unmarshal(data, &q); err != nil {
		return false, fmt.Errorf("read the queue of ComfyUI: %w", err)
	}
	return len(q.Running)+len(q.Pending) > 0, nil
}

func (g Guard) post(ctx context.Context, url string, body []byte) error {
	_, err := g.do(ctx, http.MethodPost, url, body)
	return err
}

func (g Guard) do(ctx context.Context, method, url string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	doer := g.HTTP
	if doer == nil {
		doer = httpclient.New()
	}
	res, err := doer.Do(req)
	if err != nil {
		return nil, httpclient.RedactURL(err)
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxAnswerBytes))
	if err != nil {
		return nil, err
	}
	if res.StatusCode/100 != 2 {
		return nil, fmt.Errorf("ComfyUI answered %d", res.StatusCode)
	}
	return data, nil
}
