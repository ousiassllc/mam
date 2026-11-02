// Package loras lists the LoRA files the bundled ComfyUI engine has, for the generation test's LoRA choice.
package loras

import (
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
	keep    = 5 * time.Second
	timeout = 3 * time.Second
	// loaderNode is the node class of a LoRA that the bundled workflows insert (workflow lora_model_node): its lora_name
	// input lists the files of the loras folder.
	loaderNode = "LoraLoaderModelOnly"
)

// Lister asks an engine for its LoRA files and keeps the answer for a few seconds. The zero value is ready to use.
type Lister struct {
	mu    sync.Mutex
	url   string
	at    time.Time
	names []string
	// Now is for tests (nil: time.Now).
	Now func() time.Time
}

// List returns the names of the LoRA files of the engine registered as endpoint name, sorted. It is empty when the endpoint is
// not a ComfyUI, the engine does not run (it is not started for this), or has none.
func (l *Lister) List(ctx context.Context, name string, ep localcfg.Endpoint) []string {
	if ep.Kind != localcfg.KindComfyUI {
		return nil
	}
	now := time.Now
	if l.Now != nil {
		now = l.Now
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.url == ep.BaseURL && now().Sub(l.at) < keep {
		return slices.Clone(l.names)
	}
	names, ok := fetch(ctx, name, ep)
	if !ok {
		return nil
	}
	l.url, l.at, l.names = ep.BaseURL, now(), names
	return slices.Clone(names)
}

func fetch(ctx context.Context, name string, ep localcfg.Endpoint) ([]string, bool) {
	c, err := localhttp.New(name, ep, nil, "")
	if err != nil {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	data, err := c.Request(ctx, http.MethodGet, "/object_info/"+loaderNode, nil, nil, localhttp.MaxJSONBytes)
	if err != nil {
		return nil, false
	}
	var info map[string]struct {
		Input struct {
			Required map[string]json.RawMessage `json:"required"`
		} `json:"input"`
	}
	if json.Unmarshal(data, &info) != nil {
		return nil, false
	}
	names := options(info[loaderNode].Input.Required["lora_name"])
	slices.Sort(names)
	return slices.Compact(names), true
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
