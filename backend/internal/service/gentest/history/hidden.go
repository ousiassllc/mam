package history

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
)

// The origins (record.InputOrigin.Kind) of an input image that the age gate looks at: gentest.FromPoseLibrary and gentest.FromBatchInput are these.
const (
	OriginPoseLibrary = "pose_library"
	OriginBatchInput  = "batch_input"
)

// maxOriginChain is how many copies of copies (a batch_input of a batch_input of ...) are followed to the picture's first origin.
const maxOriginChain = 16

// InputHidden says the stored input image is one the age gate keeps out of sight now: a copy of a picture of a suggestive pose series (sexy, duo)
// of the pose library while the age gate is closed (Store.HidePose), however many times the copy was taken again from an earlier batch. The copy
// stays with its batch (it comes back when the gate opens); while the gate is closed it is not listed, not served and not taken again, so that
// the name and the picture of the pose are nowhere in the history either (Issue #1020).
func (s *Store) InputHidden(ctx context.Context, in gentestrepo.Input) bool {
	return s.originHidden(ctx, decode[record.InputOrigin](in.Origin), 0)
}

func (s *Store) originHidden(ctx context.Context, o record.InputOrigin, depth int) bool {
	switch o.Kind {
	case OriginPoseLibrary:
		return s.HidePose != nil && s.HidePose(o.ID)
	case OriginBatchInput:
		if depth >= maxOriginChain {
			return false
		}
		earlier, err := s.repo.GetInput(ctx, o.ID)
		if err != nil {
			return false // the earlier copy is gone: nothing shows through it
		}
		return s.originHidden(ctx, decode[record.InputOrigin](earlier.Origin), depth+1)
	}
	return false
}
