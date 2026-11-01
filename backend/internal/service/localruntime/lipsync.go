package localruntime

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/lipsync"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
)

// MuseTalkID is the runtime that makes the lip-synced video (catalog.PurposeLipSync).
const MuseTalkID = "musetalk"

// LipSync makes the mouth of the face in the video follow the speech, with the local MuseTalk runtime: it prepares the runtime
// (starts it when it is stopped: the lazy start of §5.2), sends the request to its server on 127.0.0.1 and returns the report of the
// generation. It is the entry the Audio Director's local lip-sync provider calls (spec/architecture/model-manager/local-runtime.md §13.3).
//
// A machine that cannot run it, a runtime that is not installed or whose weights are not there are refused before anything starts, with the
// sentence and the cloud provider to use instead (*SupportError, ErrNotInstalled, *WeightsError); a server that fails to start is the
// error of the start (the log is in the runtime's View). The refusals of a generation are *lipsync.Error (errors.Is with lipsync.ErrNoFace,
// ErrOutOfMemory, ...). One generation runs at a time (the GPU is one): a second call waits for the first, or ends with ctx.
func (s *Service) LipSync(ctx context.Context, req lipsync.Request) (lipsync.Response, error) {
	if err := req.Check(); err != nil {
		return lipsync.Response{}, err
	}
	inst, err := s.lipSyncRuntime()
	if err != nil {
		return lipsync.Response{}, err
	}
	select {
	case inst.call <- struct{}{}:
		defer func() { <-inst.call }()
	case <-ctx.Done():
		return lipsync.Response{}, ctx.Err()
	}
	if err := s.Prepare(ctx, inst.rt.ID); err != nil {
		return lipsync.Response{}, err
	}
	st := inst.proc.Status()
	if st.State != proc.StateRunning || st.Port == 0 {
		return lipsync.Response{}, fmt.Errorf("%s が動いていません（状態: %s）", inst.rt.Name, st.State)
	}
	c := lipsync.Client{BaseURL: "http://127.0.0.1:" + strconv.Itoa(st.Port), Token: inst.currentToken(), HTTP: s.HTTP}
	res, err := c.Lipsync(ctx, req)
	if errors.Is(err, lipsync.ErrUnreachable) {
		return res, fmt.Errorf("%s のサーバーが応答しません（途中で止まった可能性があります。ログを確かめてもう一度お試しください）: %w", inst.rt.Name, err)
	}
	return res, err
}

// lipSyncRuntime is the runtime that makes lip-synced video: the one of the catalog with the purpose lip_sync.
func (s *Service) lipSyncRuntime() (*instance, error) {
	for _, inst := range s.instances {
		if inst.rt.Purpose == catalog.PurposeLipSync {
			return inst, nil
		}
	}
	return nil, ErrNotFound
}
