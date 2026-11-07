// Package llamaserver is the llama.cpp server that mam installs, starts and stops itself (the local runtime "llama-cpp" of package localruntime,
// spec/architecture/model-manager/local-runtime.md §17) as the LLM provider "local" uses it (spec/architecture/local-llm.md §10): where it is, with the
// key of its current start, started on the first call. The same Server is the Ollama of mam (the runtime "ollama", local-runtime.md §19; #1105): it differs in the
// runtime it names, what it calls itself, that it has no key (Ollama has no API key) and that it answers to every model Ollama holds, not to one chosen. It makes room on the GPU before it starts (package gpufree: ComfyUI keeps the models of its
// last generation in video memory, and a language model of 7 to 15 GiB does not fit beside them on a 16 GB GPU) and waits while ComfyUI is
// generating, because the two never run at the same moment. It is a package of its own so that llmlocal and localruntime stay small.
package llamaserver

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

// RuntimeID is the llama.cpp runtime of the catalog (data/runtimes/llama-cpp.json), OllamaID the Ollama one (data/runtimes/ollama.json).
const (
	RuntimeID = "llama-cpp"
	OllamaID  = "ollama"
)

// Defaults of the wait for ComfyUI to finish a generation: the first call of an Agent waits this long for the GPU (its own context may end it
// sooner) before it says the GPU is busy.
const (
	DefaultMaxWait   = 5 * time.Minute
	DefaultPollEvery = 3 * time.Second
)

// Runtime is the part of *localruntime.Service the server uses.
type Runtime interface {
	// Prepare starts the runtime when it is stopped (the lazy start) and waits until it answers.
	Prepare(ctx context.Context, id string) error
	Status(ctx context.Context, id string) (localruntime.View, error)
	// Token is the token of the server's current start: llama-server reads it as its API key (LLAMA_API_KEY) and answers no request without it.
	Token(id string) (string, error)
	Stop(id string) error
}

// Server is the managed llama.cpp server, or with ID and Label the managed Ollama. It implements llmlocal.Managed.
type Server struct {
	Runtime Runtime
	// ID is the runtime the server is (default RuntimeID) and Label what the screens call it (default "llama.cpp"; "Ollama" for OllamaID).
	ID, Label string
	// NoKey says the server has no API key: no key is asked of the runtime, and none is sent (Ollama has none; llama-server answers only a call that carries its key).
	NoKey bool
	// Models says which models the server answers to now (nil: only the one the runtime's choice names, llama-server's --alias). It is read without starting the server.
	Models func(ctx context.Context) []string
	// MakeRoom runs before the server is started, only when it is not running (nil for nothing): busy says the bundled ComfyUI is generating
	// (gpufree.Guard.MakeRoom frees its memory when it is idle).
	MakeRoom func(ctx context.Context) (busy bool, err error)
	// MaxWait and PollEvery tune the wait for a busy ComfyUI (defaults: DefaultMaxWait, DefaultPollEvery).
	MaxWait, PollEvery time.Duration
}

// UnavailableError is why the server cannot run now, in words for the user. The calls of the Agents see it as a local server that did not answer.
type UnavailableError struct {
	Detail string
	Err    error
	// Label is the server ("" for llama.cpp).
	Label string
}

func (e *UnavailableError) Error() string {
	if e.Label != "" {
		return "この PC の " + e.Label + " を使えません: " + e.Detail
	}
	return "この PC の llama.cpp を使えません: " + e.Detail
}
func (e *UnavailableError) Unwrap() error { return e.Err }

// Endpoint starts the server when it is stopped and returns its address and the key of this start. The model in force is the one the model manager's
// choice names (localruntime / runtimechoices); changing it stops the server, so the next call loads the other one. The start does not belong to the
// call: loading a model takes minutes (start_timeout_sec of the runtime bounds it), longer than the timeout of one call, and a start that the call's
// context ended would kill the half-loaded server and begin again at the next call. The call only waits; when its context ends first the start goes on,
// and the next call joins it.
func (s Server) Endpoint(ctx context.Context) (string, string, error) {
	if s.MakeRoom != nil {
		if v, err := s.Runtime.Status(ctx, s.id()); err == nil && v.Process.State != proc.StateRunning {
			if err := s.room(ctx); err != nil {
				return "", "", err
			}
		}
	}
	if err := s.prepare(ctx); err != nil {
		return "", "", s.unavailable(ctx, err)
	}
	v, err := s.Runtime.Status(ctx, s.id())
	if err != nil {
		return "", "", s.unavailable(ctx, err)
	}
	// A server that died between Prepare and Status keeps its address in the Failed state: only a running one is an address to call.
	if v.Process.State != proc.StateRunning || v.Process.URL == "" {
		return "", "", &UnavailableError{Detail: s.label() + " のアドレスを取得できませんでした", Label: s.Label}
	}
	if s.NoKey {
		return v.Process.URL, "", nil
	}
	key, err := s.Runtime.Token(s.id())
	if err != nil {
		return "", "", s.unavailable(ctx, err)
	}
	if key == "" { // the server would answer every call: say so here, never talk to a server that has no key
		return "", "", &UnavailableError{Detail: s.label() + " の合言葉を取得できませんでした", Label: s.Label}
	}
	return v.Process.URL, key, nil
}

// prepare starts the server (Runtime.Prepare) under a context of its own, not the call's: the call waits for the result, or for its own context to end,
// whichever is first. Runtime.Prepare joins a start already in progress, so a call after an abandoned one waits for the same start, never begins another.
func (s Server) prepare(ctx context.Context) error {
	done := make(chan error, 1)
	startCtx := context.WithoutCancel(ctx)
	go func() { done <- s.Runtime.Prepare(startCtx, s.id()) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// room waits while the bundled ComfyUI is generating, up to MaxWait (the context of the call may end it sooner); a ComfyUI that is idle is asked
// to free its memory by MakeRoom.
func (s Server) room(ctx context.Context) error {
	wait, poll := s.MaxWait, s.PollEvery
	if wait <= 0 {
		wait = DefaultMaxWait
	}
	if poll <= 0 {
		poll = DefaultPollEvery
	}
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	tick := time.NewTicker(poll)
	defer tick.Stop()
	for {
		busy, err := s.MakeRoom(ctx)
		switch {
		case err != nil:
			return err
		case !busy:
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return &UnavailableError{Detail: "同梱の ComfyUI が画像・動画を生成している最中で、GPU のメモリが空きません。生成が終わってから、もう一度実行してください"}
		case <-tick.C:
		}
	}
}

// unavailable is the error of the runtime manager as the sentence of an UnavailableError; the context of the call ending stays as it is.
func (s Server) unavailable(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return &UnavailableError{Detail: explain(s.label(), err), Err: err, Label: s.Label}
}

// id is the runtime of the server and label what the screens call it.
func (s Server) id() string    { return cmp.Or(s.ID, RuntimeID) }
func (s Server) label() string { return cmp.Or(s.Label, "llama.cpp") }

// Explain says in Japanese why the runtime manager refused the llama.cpp of mam, with what to do.
func Explain(err error) string { return explain("llama.cpp", err) }

// ExplainOllama is Explain for the Ollama of mam.
func ExplainOllama(err error) string { return explain("Ollama", err) }

func explain(label string, err error) string {
	var (
		support *localruntime.SupportError
		weights *localruntime.WeightsError
		busy    *localruntime.BusyError
	)
	switch {
	case errors.As(err, &support):
		return support.Error()
	case errors.As(err, &busy):
		return busy.Reason
	case errors.Is(err, localruntime.ErrBusy):
		return label + " を導入している最中です。終わってから、もう一度実行してください"
	case errors.Is(err, localruntime.ErrNotInstalled), errors.Is(err, localruntime.ErrNotFound):
		return label + " が導入されていません。モデルマネージャーの「ローカル LLM」で " + label + " を導入してください"
	case errors.As(err, &weights):
		names := make([]string, 0, len(weights.Assets))
		for _, a := range weights.Assets {
			names = append(names, a.Name)
		}
		return "モデルの重みが足りません（" + strings.Join(names, "、") + "）。モデルマネージャーの「ローカル LLM」の llama.cpp で「重みを導入」を実行してください"
	}
	return err.Error()
}

// Stop stops the server (a stopped one is a no-op): the video memory is given back before an image or video generation.
func (s Server) Stop(context.Context) error { return s.Runtime.Stop(s.id()) }

// Info says where the server stands, without starting it.
func (s Server) Info(ctx context.Context) llmlocal.ManagedInfo {
	v, err := s.Runtime.Status(ctx, s.id())
	if err != nil {
		return llmlocal.ManagedInfo{State: llmlocal.ManagedNotInstalled, Reason: explain(s.label(), err)}
	}
	info := llmlocal.ManagedInfo{Installing: v.InstallJob != "", URL: v.Process.URL}
	if v.Choice != nil {
		info.Model, info.ModelName = v.Choice.ID, v.Choice.Name
	}
	if s.Models != nil {
		info.Models = s.Models(ctx)
	}
	switch {
	case !v.Support.Supported:
		info.State, info.Reason = llmlocal.ManagedUnsupported, v.Support.Message()
	case !v.Installed:
		info.State, info.Reason = llmlocal.ManagedNotInstalled, explain(s.label(), localruntime.ErrNotInstalled)
		if v.Broken {
			info.Reason = s.label() + " のファイルが見つかりません（削除されたようです）。モデルマネージャーの「ローカル LLM」で導入し直してください"
		}
	case v.Weights.State != localruntime.WeightsReady:
		info.State, info.Reason = llmlocal.ManagedNotInstalled, fmt.Sprintf("モデル（%s）の重みがそろっていません。モデルマネージャーの「ローカル LLM」の llama.cpp で「重みを導入」を実行してください", info.ModelName)
	default:
		info.State = managedState(v.Process)
		info.Reason = v.Process.Error
	}
	return info
}

func managedState(p proc.Status) llmlocal.ManagedState {
	switch p.State {
	case proc.StateRunning:
		return llmlocal.ManagedRunning
	case proc.StateStarting:
		return llmlocal.ManagedStarting
	case proc.StateFailed:
		return llmlocal.ManagedFailed
	}
	return llmlocal.ManagedStopped
}

var _ llmlocal.Managed = Server{}
