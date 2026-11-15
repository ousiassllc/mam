package configtest

func init() {
	// The core nodes of ComfyUI v0.42.1 that the speech-to-video template of Issue #1153 uses (comfy_extras/nodes_audio.py LoadAudio, nodes_audio_encoder.py
	// AudioEncoderLoader / AudioEncoderEncode and nodes_wan.py WanSoundImageToVideo, all loaded by nodes.py of that tag; each node_id was read from those files).
	coreNodes = append(coreNodes, "AudioEncoderEncode", "AudioEncoderLoader", "LoadAudio", "WanSoundImageToVideo")
}
