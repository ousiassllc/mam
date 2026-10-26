package workflows

import "github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"

// The types of the input assignment, re-exported so that the handlers (which may not import the agent packages) can
// build and read a workflow's settings.
type (
	// Media is image or video.
	Media = localcfg.Media
	// WorkflowSpec is the input assignment, the output node and the frame and video settings of a workflow.
	WorkflowSpec = localcfg.WorkflowSpec
	// Assignment says which input of which node a value of mam goes to.
	Assignment = localcfg.Assignment
	// Param is a value of mam a workflow takes.
	Param = localcfg.Param
	// VideoSettings are the lengths and resolutions of a video workflow.
	VideoSettings = localcfg.VideoSettings
)

// The kinds of a workflow and the one value that has a slot per image.
const (
	MediaImage          = localcfg.MediaImage
	MediaVideo          = localcfg.MediaVideo
	ParamReferenceImage = localcfg.ParamReferenceImage
)

// ImageParams are the values an image workflow takes, in the order of the assignment table.
func ImageParams() []Param { return localcfg.ImageParams() }

// VideoParams are the values a video workflow takes.
func VideoParams() []Param { return localcfg.VideoParams() }
