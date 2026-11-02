package stage

import (
	"maps"
	"slices"
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// encoders are the text encoder nodes of an editing workflow. pos makes the positive conditioning; neg is the node of the
// negative one, empty when pos makes both (TextEncodeQwenImage21 has outputs for the two). plus is the EditPlus encoder, which
// takes its images as image1..3 and calls them "Picture N" (the other takes images.image_N and reads <imageN> as it is).
type encoders struct {
	pos, neg string
	plus     bool
}

// negativeEncoder is the TextEncodeQwenImageEditPlus node the sampler takes its negative conditioning from.
func negativeEncoder(g localcfg.Graph, sampler string) (string, bool) {
	to, idx, ok := LinkOf(g[sampler].Inputs["negative"])
	if !ok || idx != 0 || g[to].ClassType != editPlusClass {
		return "", false
	}
	return to, true
}

// slotName is the input of an encoder that holds the image of the k-th reference of a part (from 1).
func (e encoders) slotName(k int) string {
	if e.plus {
		return "image" + strconv.Itoa(k)
	}
	return slotPrefix + strconv.Itoa(k)
}

// words is the prompt as the encoder reads it: the EditPlus encoder numbers its images "Picture N".
func (e encoders) words(s string) string {
	if !e.plus {
		return s
	}
	return imageTag.ReplaceAllString(s, "Picture $1")
}

// singleStage keeps the nodes: every reference is in the whole run.
func singleStage(g localcfg.Graph, enc encoders, uses []refUse, prompt string) error {
	n := g[enc.pos]
	n.Inputs["prompt"] = Literal(enc.words(promptFor(uses, prompt)))
	setVAE(g, n, uses)
	if enc.neg != "" {
		setVAE(g, g[enc.neg], uses)
	}
	return nil
}

// encoderFor is a copy of the encoder node src that has the references active in a part of the sampling.
func encoderFor(g localcfg.Graph, src string, enc encoders, active []refUse) localcfg.Node {
	clone := cloneNode(g[src])
	setVAE(g, clone, active)
	keepSlots(clone, g[src], active, enc)
	return clone
}

// cloneNode copies a node so that the copy's inputs can change.
func cloneNode(n localcfg.Node) localcfg.Node {
	return localcfg.Node{ClassType: n.ClassType, Inputs: maps.Clone(n.Inputs)}
}

// keepSlots links the images of the references of the part into the encoder, in their order: the n-th is <image n> of its
// prompt. src is the encoder as the template has it, whose slot k holds the image of the reference k-1.
func keepSlots(n, src localcfg.Node, active []refUse, e encoders) {
	for name := range n.Inputs {
		if slotInput.MatchString(name) {
			delete(n.Inputs, name)
		}
	}
	for k, u := range active {
		n.Inputs[e.slotName(k+1)] = src.Inputs[e.slotName(u.slot+1)]
	}
}

// seenOnly says a reference of the role is only looked at by the vision tower: its pixels do not go into the latents.
func seenOnly(role string) bool {
	return role == localcfg.RoleFace || role == localcfg.RoleStyle || role == localcfg.RolePose
}

// setVAE gives the encoder the VAE when a reference is to be reproduced (the whole picture or the one to edit): the encoder
// then also hands the picture's latents to the model. A face, a style or a pose is read with the vision tower alone (its pixels are
// not to appear in the result). The VAE is the
// one the VAEDecode node takes (the lowest ID of them when there are several), else the VAELoader with the lowest ID.
func setVAE(g localcfg.Graph, n localcfg.Node, active []refUse) {
	if !slices.ContainsFunc(active, func(u refUse) bool { return !seenOnly(u.role) }) {
		delete(n.Inputs, "vae")
		return
	}
	ids := make([]string, 0, len(g))
	for id := range g {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if g[id].ClassType != "VAEDecode" {
			continue
		}
		if to, _, ok := LinkOf(g[id].Inputs["vae"]); ok && g[to].ClassType == "VAELoader" {
			n.Inputs["vae"] = Link(to, 0)
			return
		}
	}
	for _, id := range ids {
		if g[id].ClassType == "VAELoader" {
			n.Inputs["vae"] = Link(id, 0)
			return
		}
	}
}
