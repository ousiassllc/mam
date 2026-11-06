package configtest

func init() {
	// The core nodes of ComfyUI v0.42.1 that the Mega first-and-last-frame template of Issue #1076 adds (comfy_extras/nodes_post_processing.py
	// BatchImagesNode, nodes_images.py RepeatImageBatch, nodes_mask.py MaskToImage and ImageToMask; the whole graph was accepted by the prompt validation
	// of a running v0.42.1 server on 2026-11-06, and the batch inputs are named images.image0, images.image1, ... as its Autogrow takes them).
	coreNodes = append(coreNodes, "BatchImagesNode", "RepeatImageBatch", "MaskToImage", "ImageToMask")
}
