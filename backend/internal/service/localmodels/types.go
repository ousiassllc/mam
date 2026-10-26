package localmodels

import (
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// The vocabulary of an endpoint and a model, as the layers above the service (the settings screen) use it:
// they depend on the service only, so the types of the model definition are named here.
type (
	// Kind is the protocol of an endpoint.
	Kind = localcfg.Kind
	// Media is image or video.
	Media = localcfg.Media
	// Model is the definition of a model (workflow, capabilities, prices).
	Model = localcfg.Model
	// Workflow is the ComfyUI workflow of a model with the assignments of mam's values to node inputs.
	Workflow = localcfg.Workflow
	// Assignment puts one value of mam into one input of one node.
	Assignment = localcfg.Assignment
	// Param is a value of mam a workflow can take.
	Param = localcfg.Param
	// VideoSettings are the capabilities of a video model.
	VideoSettings = localcfg.VideoSettings
	// NodeInfo is a node of a workflow with its inputs.
	NodeInfo = localcfg.NodeInfo
)

// Kinds, media and the values a workflow takes that the screen names.
const (
	KindComfyUI         = localcfg.KindComfyUI
	KindOpenAICompat    = localcfg.KindOpenAICompat
	MediaImage          = localcfg.MediaImage
	MediaVideo          = localcfg.MediaVideo
	ParamReferenceImage = localcfg.ParamReferenceImage
)

// ImageParams and VideoParams are the values of mam an image / video workflow takes, in display order.
func ImageParams() []Param { return localcfg.ImageParams() }

// VideoParams is ImageParams for a video workflow.
func VideoParams() []Param { return localcfg.VideoParams() }

// ModelID is the identifier of a model in take.model_name: "<kind>:<endpoint>/<name>".
func ModelID(kind Kind, endpoint, name string) string { return localcfg.ID(kind, endpoint, name) }

// Template is a built-in workflow.
type Template = localcfg.Template

// TemplateByID returns the built-in workflow with that id.
func TemplateByID(id string) (Template, bool) { return localcfg.TemplateByID(id) }

// Inspect lists the nodes of a workflow graph with their inputs (the choices of the assignment table).
func Inspect(graph []byte) ([]NodeInfo, error) { return localcfg.Inspect(graph) }
