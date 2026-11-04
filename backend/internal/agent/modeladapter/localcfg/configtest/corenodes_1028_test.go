package configtest

func init() {
	// The core node of ComfyUI v0.42.1 that the Mega first-frame template of Issue #1028 adds (comfy_extras/nodes_mask.py SolidMask; found, with the
	// inputs value/width/height and the MASK output the template wires, by /object_info of a running v0.42.1 server, and the whole graph was accepted
	// by that server's prompt validation on 2026-11-04).
	coreNodes = append(coreNodes, "SolidMask")
}
