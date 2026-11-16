package workflows

import (
	"os"

	"github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest/audioupload"
	workflowsvc "github.com/ousiassllc/mam/backend/internal/service/workflows"
)

// stagedAudio reads the sound file an upload ticket of the purpose audioupload.Purpose staged (Issue #1153), as the tools of the generation test take it. It returns the
// bytes and the function that consumes the staged file once the run has started (a run that was refused keeps it, so that the call can be made again).
func (h handlers) stagedAudio(id string) (data []byte, consume func(), err error) {
	if id == "" {
		return nil, func() {}, nil
	}
	if h.svc.Gentest == nil {
		return nil, nil, mapError(&workflowsvc.InvalidError{Message: "音声ファイルの入力はこのアプリでは使えません"})
	}
	path, _, err := h.svc.Gentest.StagedFile(id)
	if err != nil {
		return nil, nil, mapError(&workflowsvc.InvalidError{Message: "アップロードした音声 " + id + " はありません（使用済みか、24 時間を過ぎて消えました）。create_upload_ticket（purpose " + audioupload.Purpose + "）でもう一度アップロードしてください"})
	}
	if data, err = os.ReadFile(path); err != nil { //nolint:gosec // the path of a staged file the service named: its id has no separator
		return nil, nil, err
	}
	return data, func() { _ = os.Remove(path) }, nil
}
