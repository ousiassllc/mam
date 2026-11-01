// Package locallipsync connects the Audio Director's local lip sync (MuseTalk 1.5, package musetalk) to the local runtime manager, which
// installs, starts and stops the runtime (package localruntime). Like localspeech it does not import package service: it gives the Kind
// of the failure (*localvoice.UnavailableError) and the Director writes the sentence for the Skipped list.
package locallipsync

import (
	"context"
	"errors"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/lipsync"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/localspeech"
)

// Runtime is the part of *localruntime.Service the lip sync uses.
type Runtime interface {
	// Prepare starts the runtime when it is stopped (the lazy start) and waits until it answers.
	Prepare(ctx context.Context, id string) error
	LipSync(ctx context.Context, req lipsync.Request) (lipsync.Response, error)
}

// Runner is the MuseTalk runtime of the runtime manager, for musetalk.New.
type Runner struct{ Runtime Runtime }

// Prepare starts the runtime when it is stopped and waits until it answers. Every refusal of the manager (not installed, weights
// missing, not supported here, VRAM too small, would not start in time) is an *localvoice.UnavailableError with its Kind.
func (r Runner) Prepare(ctx context.Context) error {
	if err := r.Runtime.Prepare(ctx, localruntime.MuseTalkID); err != nil {
		return localspeech.UnavailableFor(ctx, localvoice.VendorMuseTalk, err)
	}
	return nil
}

// LipSync makes one lip-synced video. The refusals of the server (*lipsync.Error, lipsync.ErrUnreachable) and a request the entry
// rejects (lipsync.ErrBadRequest) are returned as they are, for the client to read; every other failure is the runtime manager's (it
// would not start, was not installed, ...) and is an *localvoice.UnavailableError, as in Prepare.
func (r Runner) LipSync(ctx context.Context, req lipsync.Request) (lipsync.Response, error) {
	res, err := r.Runtime.LipSync(ctx, req)
	var refused *lipsync.Error
	if err == nil || errors.As(err, &refused) || errors.Is(err, lipsync.ErrUnreachable) || errors.Is(err, lipsync.ErrBadRequest) {
		return res, err
	}
	return res, localspeech.UnavailableFor(ctx, localvoice.VendorMuseTalk, err)
}
