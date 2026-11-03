package configtest

func init() {
	// The core nodes of ComfyUI v0.42.1 that the uncensored video templates of Issue #1012 use (comfy_extras/nodes_custom_sampler.py and
	// nodes_wan.py; each was also found, with the inputs and outputs the templates wire, by /object_info of a running v0.42.1 server).
	coreNodes = append(coreNodes, "BasicGuider", "TrimVideoLatent", "WanVaceToVideo")
}
