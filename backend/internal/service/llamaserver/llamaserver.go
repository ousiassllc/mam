// Package llamaserver is the llama.cpp server that mam installs, starts and stops itself (the local runtime "llama-cpp" of package localruntime,
// spec/architecture/model-manager/local-runtime.md §17) as the LLM provider "local" uses it (spec/architecture/local-llm.md §10): where it is, with the
// key of its current start, started on the first call. It makes room on the GPU before it starts (package gpufree: ComfyUI keeps the models of its
// last generation in video memory, and a language model of 7 to 15 GiB does not fit beside them on a 16 GB GPU) and waits while ComfyUI is
// generating, because the two never run at the same moment. It is a package of its own so that llmlocal and localruntime stay small.
package llamaserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

// RuntimeID is the llama.cpp runtime of the catalog (data/runtimes/llama-cpp.json).
const RuntimeID = "llama-cpp"

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

// Server is the managed llama.cpp server. It implements llmlocal.Managed.
type Server struct {
	Runtime Runtime
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
}

func (e *UnavailableError) Error() string {
	return "この PC の llama.cpp を使えません: " + e.Detail
}
func (e *UnavailableError) Unwrap() error { return e.Err }

// Endpoint starts the server when it is stopped and returns its address and the key of this start. The model in force is the one the model manager's
// choice names (localruntime / runtimechoices); changing it stops the server, so the next call loads the other one.
func (s Server) Endpoint(ctx context.Context) (string, string, error) {
	if s.MakeRoom != nil {
		if v, err := s.Runtime.Status(ctx, RuntimeID); err == nil && v.Process.State != proc.StateRunning {
			if err := s.room(ctx); err != nil {
				return "", "", err
			}
		}
	}
	if err := s.Runtime.Prepare(ctx, RuntimeID); err != nil {
		return "", "", s.unavailable(ctx, err)
	}
	v, err := s.Runtime.Status(ctx, RuntimeID)
	if err != nil {
		return "", "", s.unavailable(ctx, err)
	}
	// A server that died between Prepare and Status keeps its address in the Failed state: only a running one is an address to call.
	if v.Process.State != proc.StateRunning || v.Process.URL == "" {
		return "", "", &UnavailableError{Detail: "llama.cpp のアドレスを取得できませんでした"}
	}
	key, err := s.Runtime.Token(RuntimeID)
	if err != nil {
		return "", "", s.unavailable(ctx, err)
	}
	if key == "" { // the server would answer every call: say so here, never talk to a server that has no key
		return "", "", &UnavailableError{Detail: "llama.cpp の合言葉を取得できませんでした"}
	}
	return v.Process.URL, key, nil
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
	return &UnavailableError{Detail: Explain(err), Err: err}
}

// Explain says in Japanese why the runtime manager refused, with what to do.
func Explain(err error) string {
	var (
		support *localruntime.SupportError
		weights *localruntime.WeightsError
	)
	switch {
	case errors.As(err, &support):
		return support.Error()
	case errors.Is(err, localruntime.ErrBusy):
		return "llama.cpp を導入している最中です。終わってから、もう一度実行してください"
	case errors.Is(err, localruntime.ErrNotInstalled), errors.Is(err, localruntime.ErrNotFound):
		return "llama.cpp が導入されていません。モデルマネージャーの「ローカル」で llama.cpp を導入してください"
	case errors.As(err, &weights):
		names := make([]string, 0, len(weights.Assets))
		for _, a := range weights.Assets {
			names = append(names, a.Name)
		}
		return "モデルの重みが足りません（" + strings.Join(names, "、") + "）。モデルマネージャーの「ローカル」の llama.cpp で「重みを導入」を実行してください"
	}
	return err.Error()
}

// Stop stops the server (a stopped one is a no-op): the video memory is given back before an image or video generation.
func (s Server) Stop(context.Context) error { return s.Runtime.Stop(RuntimeID) }

// Info says where the server stands, without starting it.
func (s Server) Info(ctx context.Context) llmlocal.ManagedInfo {
	v, err := s.Runtime.Status(ctx, RuntimeID)
	if err != nil {
		return llmlocal.ManagedInfo{State: llmlocal.ManagedNotInstalled, Reason: Explain(err)}
	}
	info := llmlocal.ManagedInfo{}
	if v.Choice != nil {
		info.Model, info.ModelName = v.Choice.ID, v.Choice.Name
	}
	switch {
	case !v.Support.Supported:
		info.State, info.Reason = llmlocal.ManagedUnsupported, v.Support.Message()
	case !v.Installed:
		info.State, info.Reason = llmlocal.ManagedNotInstalled, Explain(localruntime.ErrNotInstalled)
		if v.Broken {
			info.Reason = "llama.cpp のファイルが見つかりません（削除されたようです）。モデルマネージャーの「ローカル」で導入し直してください"
		}
	case v.Weights.State != localruntime.WeightsReady:
		info.State, info.Reason = llmlocal.ManagedNotInstalled, fmt.Sprintf("モデル（%s）の重みがそろっていません。モデルマネージャーの「ローカル」の llama.cpp で「重みを導入」を実行してください", info.ModelName)
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
