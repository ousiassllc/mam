package gentest

import "github.com/ousiassllc/mam/backend/internal/service/gentest/record"

// FaceSimilarity is whether the SFace similarity of a result with a face reference is measured right now, and what a result records while it is not (Issue #1026). The models can
// be installed and removed while mam runs, so it is asked every time and never cached.
func (s *Service) FaceSimilarity() record.FaceSimilarityStatus {
	off := s.FaceOff.Now()
	return record.StatusOf(off == nil && s.FaceScore != nil, off)
}
