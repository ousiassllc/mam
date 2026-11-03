package runner

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/facefusion"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
)

// FaceSwapInput is one swap: the picture to put the face into (Target), the picture of the face (Face) and the FaceFusion models.
type FaceSwapInput struct {
	Face, Target modeladapter.Input
	Swapper      string
	Detector     string
	// Enhancer is the face restoration model; facefusion.EnhancerNone runs none (an empty one would be the default).
	Enhancer string
}

// FaceSwapFunc runs FaceFusion (an external tool, spec/architecture/gentest/face-swap.md) on one swap and returns the picture it made.
// The service gives it when it has a FaceFusion; without one a result of "swap the face" fails with a reason that says so.
type FaceSwapFunc func(ctx context.Context, in FaceSwapInput) (data []byte, mimeType string, err error)

// errNoFaceSwap is why a swap cannot run when the service has no FaceFusion.
var errNoFaceSwap = errors.New("FaceFusion が設定されていないため、顔を入れ替えられません")

// faceSwap makes one result of "swap the face": the input of the role "edit" is the picture and the one of the role "face" the face.
func (br *batchRun) faceSwap(ctx context.Context, r gentestrepo.Result, p record.ResultParams) (file, mime string, size int64, err error) {
	s := br.s
	if s.FaceSwap == nil {
		return "", "", 0, errNoFaceSwap
	}
	in := FaceSwapInput{Swapper: p.FaceSwap.Swapper, Detector: p.FaceSwap.Detector, Enhancer: cmp.Or(p.FaceSwap.Enhancer, facefusion.EnhancerNone)} // the record has "" for none
	var haveFace, haveTarget bool
	for _, l := range br.inputsOf(r) {
		switch l.role {
		case gentestrepo.RoleFace:
			in.Face, haveFace = l.in, true
		case gentestrepo.RoleEdit:
			in.Target, haveTarget = l.in, true
		}
	}
	if !haveFace || !haveTarget {
		return "", "", 0, errors.New("顔の入れ替えには、顔の画像と入れ替え先の画像の両方が要ります")
	}
	data, mime, err := s.FaceSwap(ctx, in)
	if err != nil {
		return "", "", 0, err
	}
	file = r.ID + extensionFor(mime)
	if err := fsutil.WriteFileAtomic(filepath.Join(br.dir, file), data, 0o600); err != nil {
		return "", "", 0, fmt.Errorf("store the image: %w", err)
	}
	br.scoreFace(ctx, r, data)
	return file, mime, int64(len(data)), nil
}
