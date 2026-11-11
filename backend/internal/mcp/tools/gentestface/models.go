package gentestface

import (
	"context"
	"errors"
	"net/http"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/facemodels"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// registerModels adds the tools of the face check models (the model manager's "顔チェック用モデル", Issue #1026). Their state is read from list_generation_models
// (face_similarity.models), as the state of the pose library is read from get_pose_library: here it is one more field of a tool the agent calls before every run anyway.
func registerModels(r *mcpkit.Registrar, h handlers) {
	mcpkit.Write(r, facemodels.ToolName,
		"顔チェック用モデル（YuNet の顔検出と SFace の顔照合。OpenCV Zoo の ONNX、約 37 MB）をダウンロードして導入する。導入すると、再起動なしで、役割 face の参照を付けた単発生成の結果に face_similarity の数値が付く。導入前に list_generation_models の face_similarity.models（取得元・大きさ・ライセンス・注意）を利用者に見せ、利用者が確認して同意したときだけ accepted=true にする（false だと何も取得せず validation）。冪等（揃っていれば何もしない。SHA-256 が固定した値と合うことを確かめる）。終わるまで応答しない。ONNX Runtime のライブラリが無いときは、導入しても測れない（face_similarity.models.engine_off が runtime_unavailable）",
		h.installModels, mcpkit.OpenWorld(), mcpkit.Idempotent())
	mcpkit.Destroy(r, "remove_face_models",
		"導入した顔チェック用モデルの 2 つのファイルを削除する（フォルダの中のほかのファイルは残す）。導入していなければ何もしない。導入中は conflict。削除すると顔の類似度はオフになり、役割 face の参照を付けた結果の face_similarity.skipped は models_missing になる。履歴の結果は残る",
		h.removeModels)
}

func (h handlers) faceModels() (*facemodels.Service, error) {
	if h.svc.Gentest == nil || h.svc.Gentest.FaceModels == nil {
		return nil, &mcpkit.ToolError{Code: mcpkit.CodeUnavailable, Message: "顔チェック用モデルはこのアプリでは使えません", Status: http.StatusServiceUnavailable}
	}
	return h.svc.Gentest.FaceModels, nil
}

// mapModelsError turns the refusals of the face models into the codes a tool answers with.
func mapModelsError(err error) error {
	var (
		invalid  *gterr.InvalidError
		conflict *gterr.ConflictError
	)
	switch {
	case err == nil:
		return nil
	case errors.As(err, &invalid):
		return &mcpkit.ToolError{Code: mcpkit.CodeValidation, Message: invalid.Message, Status: http.StatusUnprocessableEntity}
	case errors.As(err, &conflict):
		return &mcpkit.ToolError{Code: mcpkit.CodeConflict, Message: conflict.Message, Status: http.StatusConflict}
	}
	return mcpkit.MapError(err)
}

// InstallModelsInput is the agreement of the user.
type InstallModelsInput struct {
	Accepted bool `json:"accepted" jsonschema:"利用者がライセンス（YuNet は MIT・SFace は Apache-2.0。学習データの権利は一次情報に無いという注意つき）と取得元を確認し、ダウンロードに同意したときだけ true"`
}

func (h handlers) installModels(ctx context.Context, in InstallModelsInput) (facemodels.View, error) {
	s, err := h.faceModels()
	if err != nil {
		return facemodels.View{}, err
	}
	if err := s.Install(ctx, in.Accepted, nil); err != nil {
		return facemodels.View{}, mapModelsError(err)
	}
	return s.Status().View(), nil
}

func (h handlers) removeModels(_ context.Context, _ Input) (facemodels.View, error) {
	s, err := h.faceModels()
	if err != nil {
		return facemodels.View{}, err
	}
	if err := s.Remove(); err != nil {
		return facemodels.View{}, mapModelsError(err)
	}
	return s.Status().View(), nil
}
