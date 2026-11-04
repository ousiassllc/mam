// Package facestatus is the face similarity's part of list_generation_models (Issue #1026): whether the SFace similarity of a result with a face reference is measured now,
// what a result records while it is not, and the state of the face check models the model manager installs. It is a package of its own because mcp/tools/gentest is at its
// size limit.
package facestatus

import (
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/facemodels"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
)

// Status is the face similarity's status with the state of the face check models (nil Models: the app has no installer for them).
type Status struct {
	record.FaceSimilarityStatus
	Models *facemodels.View `json:"models,omitempty" jsonschema:"顔チェック用モデル（YuNet・SFace）の導入の状態・導入先・ライセンス・エンジンが読めているか。モデルマネージャーの「顔チェック用モデル」と同じ"`
}

// Of is the status now: asked at every call, because the models are installed and removed while mam runs.
func Of(g *gentest.Service) Status {
	out := Status{FaceSimilarityStatus: g.FaceSimilarity()}
	if g.FaceModels != nil {
		v := g.FaceModels.Status().View()
		out.Models = &v
	}
	return out
}
