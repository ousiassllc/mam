package comfyui

import (
	"encoding/json"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
)

// GraphFor is the API-format graph a generation of the image request would send to the server for the workflow (Issue #977: "今の設定を ComfyUI で開く"):
// the prompt, the seed, the size, the sampling values, the LoRA and the exposed inputs are in it, built by the same code a run uses. No image is
// uploaded: a slot for an input image that the workflow cannot run without (ControlNet, IP-Adapter, inpaint: the image and the mask) keeps its nodes
// and the file the workflow holds, which the user replaces on the node screen; the optional slots of an editing workflow and the slot of an
// img2img workflow (it has a text-to-image form) are left out as a run without images leaves them. A request without a seed gets a new one.
func GraphFor(wf localcfg.Workflow, req modeladapter.ImageRequest) (json.RawMessage, error) {
	b := &Backend{Client: &localhttp.Client{Name: "graph"}, Model: localcfg.Model{Media: localcfg.MediaImage, Workflow: &wf}}
	v := b.imageValues(req)
	v.keepSlots = true
	return b.build(v, nil)
}
