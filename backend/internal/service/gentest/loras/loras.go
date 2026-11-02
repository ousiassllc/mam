// Package loras lists the LoRA files the bundled ComfyUI engine has, for the generation test's LoRA choice.
package loras

import (
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
)

// Tuning of the list: how long an answer is kept (the screen and the tools ask on every render) and how long the engine may
// take to answer (a stopped engine refuses at once; one that hangs must not hold the screen).
const (
	keep           = 5 * time.Second
	defaultTimeout = 3 * time.Second
	// loaderNode is the node class of a LoRA that the bundled workflows insert (workflow lora_model_node): its lora_name
	// input lists the files of the loras folder.
	loaderNode = "LoraLoaderModelOnly"
)

// Lister asks an engine for its LoRA files and keeps the answer for a few seconds, a failed one too (an engine that hangs
// costs one wait per keep, not one per call). Callers that come while a fetch runs wait for that fetch instead of starting
// their own; nothing is locked while the engine answers. The zero value is ready to use.
type Lister struct {
	mu     sync.Mutex
	url    string
	at     time.Time
	names  []string
	flight *flight
	// Now and Timeout are for tests (nil / 0: time.Now / 3 s).
	Now     func() time.Time
	Timeout time.Duration
}

// flight is a fetch under way: done is closed once names (nil on failure) are set.
type flight struct {
	url   string
	done  chan struct{}
	names []string
}

// List returns the names of the LoRA files of the engine registered as endpoint name, sorted. It is empty when the endpoint is
// not a ComfyUI, the engine does not run (it is not started for this), does not answer in time, or has none.
func (l *Lister) List(ctx context.Context, name string, ep localcfg.Endpoint) []string {
	if ep.Kind != localcfg.KindComfyUI {
		return nil
	}
	now := time.Now
	if l.Now != nil {
		now = l.Now
	}
	l.mu.Lock()
	if !l.at.IsZero() && l.url == ep.BaseURL && now().Sub(l.at) < keep {
		defer l.mu.Unlock()
		return slices.Clone(l.names)
	}
	if f := l.flight; f != nil && f.url == ep.BaseURL {
		l.mu.Unlock()
		select {
		case <-f.done:
			return slices.Clone(f.names)
		case <-ctx.Done():
			return nil
		}
	}
	f := &flight{url: ep.BaseURL, done: make(chan struct{})}
	l.flight = f
	l.mu.Unlock()

	// The fetch is shared by every waiter: it does not stop when the caller that started it goes away.
	f.names = fetch(context.WithoutCancel(ctx), name, ep, cmp.Or(l.Timeout, defaultTimeout))

	l.mu.Lock()
	l.url, l.at, l.names = ep.BaseURL, now(), f.names
	if l.flight == f {
		l.flight = nil
	}
	l.mu.Unlock()
	close(f.done)
	return slices.Clone(f.names)
}

// fetch is the sorted LoRA names the engine lists (nil: it could not be asked or answered something else).
func fetch(ctx context.Context, name string, ep localcfg.Endpoint, timeout time.Duration) []string {
	c, err := localhttp.New(name, ep, nil, "")
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	data, err := c.Request(ctx, http.MethodGet, "/object_info/"+loaderNode, nil, nil, localhttp.MaxJSONBytes)
	if err != nil {
		return nil
	}
	var info map[string]struct {
		Input struct {
			Required map[string]json.RawMessage `json:"required"`
		} `json:"input"`
	}
	if json.Unmarshal(data, &info) != nil {
		return nil
	}
	names := options(info[loaderNode].Input.Required["lora_name"])
	slices.Sort(names)
	return slices.Compact(names)
}

// options are the choices of a combo input: [[a, b, ...], {...}] (older ComfyUI) or ["COMBO", {"options": [a, b, ...]}].
func options(spec json.RawMessage) []string {
	var parts []json.RawMessage
	if json.Unmarshal(spec, &parts) != nil || len(parts) == 0 {
		return nil
	}
	var names []string
	if json.Unmarshal(parts[0], &names) == nil {
		return names
	}
	if len(parts) > 1 {
		var extra struct {
			Options []string `json:"options"`
		}
		if json.Unmarshal(parts[1], &extra) == nil {
			return extra.Options
		}
	}
	return nil
}
