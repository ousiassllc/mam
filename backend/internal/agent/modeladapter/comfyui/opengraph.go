package comfyui

import (
	"encoding/json"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
)

// GraphFor is the API-format graph a generation of the image request would send to the server for the workflow (Issue #977: "今の設定を ComfyUI で開く"):
// the prompt, the seed, the size, the sampling values, the LoRA and the exposed inputs are in it, built by the same code a run uses. No image is
// uploaded, so a slot for an input image keeps the file the workflow holds. A request without a seed gets a new one.
func GraphFor(wf localcfg.Workflow, req modeladapter.ImageRequest) (json.RawMessage, error) {
	b := &Backend{Client: &localhttp.Client{Name: "graph"}, Model: localcfg.Model{Media: localcfg.MediaImage, Workflow: &wf}}
	return b.build(b.imageValues(req), nil)
}
