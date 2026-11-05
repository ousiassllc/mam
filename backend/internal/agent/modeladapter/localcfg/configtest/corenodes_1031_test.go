package configtest

func init() {
	// The core nodes of ComfyUI v0.42.1 that the video enhancement templates of Issue #1031 use (comfy_extras/nodes_seedvr.py,
	// nodes_frame_interpolation.py and nodes_video.py, all three loaded by nodes.py of that tag; each node_id was read from those files).
	coreNodes = append(coreNodes, "FrameInterpolate", "FrameInterpolationModelLoader", "GetVideoComponents", "LoadVideo",
		"SeedVR2Conditioning", "SeedVR2PostProcessing", "SeedVR2Preprocess", "SeedVR2TemporalChunk", "SeedVR2TemporalMerge")
}
