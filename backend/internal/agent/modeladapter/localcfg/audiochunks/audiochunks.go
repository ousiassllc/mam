// Package audiochunks describes a video workflow whose clip is as long as its sound (Issue #1153, Wan 2.2 S2V): the graph makes one chunk of the clip, and to cover
// a longer sound mam adds the "Extend" part of Comfy's own template (video_wan2_2_14B_s2v: WanSoundImageToVideoExtend, a KSampler and a LatentConcat) once for every
// chunk after the first. The workflow's JSON holds the one-chunk graph and this Spec, which says where the chain starts and what one more chunk is made of; the
// frame arithmetic is here, the editing of the graph is in comfyui/extend. It has no dependency on localcfg (localcfg holds the Spec in Workflow.AudioChunks).
package audiochunks

import (
	"encoding/json"
	"math"
)

// Prev is the value of an input of an extension node that stands for "the latent of everything made so far": the output 0 of Spec.Start for the first extension,
// of the previous extension's Spec.Output after that.
const Prev = "$prev"

// FramesPerLatent is how many video frames a latent frame stands for in Wan (the VAE compresses time by 4).
const FramesPerLatent = 4

// MaxTrim is the largest "length" an ImageFromBatch node takes (ComfyUI's own limit of the input): a clip longer than that is not trimmed.
const MaxTrim = 4096

// Node is one node of the extension, as in an API-format graph. The ids of an extension's nodes are its own (Spec.Extension keys); a link to a node that is not one of them
// is a link to the base graph.
type Node struct {
	ClassType string                     `json:"class_type"`
	Inputs    map[string]json.RawMessage `json:"inputs"`
}

// Spec is the audio-length part of a workflow.
type Spec struct {
	// ChunkLength is the "length" (frames) of one chunk: the first chunk's WanSoundImageToVideo and the Extend nodes take it. Wan needs 4n+1 and at least 73 (the
	// model's own check); the official template uses 77, which is 20 latent frames and an audio window of 5 seconds at 16 frames per second.
	ChunkLength int `json:"chunk_length"`
	// Start is the node whose output 0 is the latent of the first chunk (the sampler). Whatever takes that latent in the base graph (the VAEDecode) is moved to
	// the last extension's Output.
	Start string `json:"start"`
	// Sampler is the sampler node (KSampler) whose seed, steps, cfg, sampler_name and scheduler (after the values of the run are put in) are copied to the sampler nodes of the extension.
	Sampler string `json:"sampler"`
	// Trim is an ImageFromBatch node between the decoder and the video: mam puts the frame count of the sound into its "length" so that the last chunk's surplus is cut.
	Trim string `json:"trim"`
	// Extension is one more chunk: nodes with ids of their own. An input whose value is the string Prev is the latent so far; a link to another node of the extension
	// stays inside it; a link to a node of the base graph (the conditioning, the model, the audio encoding, the picture) stays as it is.
	Extension map[string]Node `json:"extension"`
	// Output is the node of the extension whose output 0 is the latent so far including the new chunk (the LatentConcat).
	Output string `json:"output"`
}

// LatentFrames is the number of latent frames of a chunk.
func (s Spec) LatentFrames() int { return (s.ChunkLength-1)/FramesPerLatent + 1 }

// Window is the number of video frames one chunk adds to the clip: each chunk takes an audio window of its latent frames times four, and the clip of n chunks
// has Window*n-3 frames (the first latent frame is one picture; Wan's 4n+1 rule: 4*(latent-1)+1), so the first chunk of 77 shows 77 frames and the clip of
// two chunks 157.
func (s Spec) Window() int { return s.LatentFrames() * FramesPerLatent }

// FramesFor is the number of video frames the sound of the given length needs at fps: the length times fps, rounded up (at least 1).
func FramesFor(seconds float64, fps int) int {
	return max(1, int(math.Ceil(seconds*float64(fps)-1e-6))) // the 1e-6 keeps 3.0 s at 16 fps at 48 frames, not 49
}

// Chunks is how many chunks cover the number of frames: the least n with Window*n-3 >= frames (at least 1).
func (s Spec) Chunks(frames int) int {
	w := s.Window()
	return max(1, (frames+3+w-1)/w)
}
