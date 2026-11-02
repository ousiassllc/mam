package stage

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// A FLUX.2 editing workflow (klein) has no vision tower: a reference joins the conditioning as a latent, one ReferenceLatent node
// for each reference in the positive and in the negative chain (the template has a slot with its scaler, VAEEncode and
// ReferenceLatent pair for every reference; dropOrphans took out the slots the run has no image for). The staging keeps the
// same meaning of the strength as the Qwen one: a reference of the strength s stays in the conditioning for the first s of the
// steps, so the sampling is cut where a reference drops out and each part has the chains of the references still in it.

// refChains are the nodes of a ReferenceLatent workflow the staging rebuilds: the guider and the sampler that uses it, the
// positive and negative chains (the ReferenceLatent nodes from the encoder outward, one for each reference) and the node the
// negative chain starts from.
type refChains struct {
	guider, sampler string
	pos, neg        []string
	negStart        string
}

// stageReferenceLatent rewrites the workflow for the references of the run. enc is the prompt's CLIPTextEncode.
func stageReferenceLatent(g localcfg.Graph, enc string, count int, opts localcfg.ImageOptions, prompt string) error {
	c, err := findRefChains(g, enc)
	if err != nil {
		return err
	}
	if len(c.pos) != count || len(c.neg) != count {
		return fmt.Errorf("the workflow has %d and %d ReferenceLatent nodes for %d references", len(c.pos), len(c.neg), count)
	}
	sigmas, _, _ := LinkOf(g[c.sampler].Inputs["sigmas"])
	total := intInput(g[sigmas], "steps")
	uses := make([]refUse, count)
	for i := range count {
		o := opts.Ref(i)
		uses[i] = refUse{slot: i, role: o.Role, steps: stepsFor(o.StrengthOrDefault(), total)}
	}
	cuts := stageCuts(uses, total)
	snapToCuts(uses, cuts, total)
	if total <= 0 || len(cuts) == 0 {
		g[enc].Inputs["text"] = Literal(imageWords(promptFor(uses, prompt)))
		return nil
	}
	return multiStageChains(g, enc, c, uses, cuts, total, prompt)
}

// findRefChains walks back from the CFGGuider whose positive conditioning is the encoder (through ReferenceLatent nodes only)
// and from its negative, and the SamplerCustomAdvanced that runs the guider.
func findRefChains(g localcfg.Graph, enc string) (refChains, error) {
	ids := make([]string, 0, len(g))
	for id := range g {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, cmp.Compare[string])
	for _, id := range ids {
		if g[id].ClassType != "CFGGuider" {
			continue
		}
		pos, posStart := chainBack(g, g[id].Inputs["positive"])
		if posStart != enc {
			continue
		}
		neg, negStart := chainBack(g, g[id].Inputs["negative"])
		if negStart == "" {
			return refChains{}, fmt.Errorf("the negative conditioning of the guider %s is not a node", id)
		}
		for _, s := range ids {
			if g[s].ClassType != "SamplerCustomAdvanced" {
				continue
			}
			if to, _, ok := LinkOf(g[s].Inputs["guider"]); ok && to == id {
				return refChains{guider: id, sampler: s, pos: pos, neg: neg, negStart: negStart}, nil
			}
		}
		return refChains{}, fmt.Errorf("no SamplerCustomAdvanced runs the guider %s", id)
	}
	return refChains{}, fmt.Errorf("no CFGGuider takes the output of %s", enc)
}

// chainBack follows a conditioning link back through ReferenceLatent nodes: the nodes from the start outward, and the node
// the chain starts from.
func chainBack(g localcfg.Graph, from json.RawMessage) (chain []string, start string) {
	for {
		id, _, ok := LinkOf(from)
		if !ok {
			return chain, ""
		}
		if g[id].ClassType != "ReferenceLatent" {
			slices.Reverse(chain)
			return chain, id
		}
		chain = append(chain, id)
		from = g[id].Inputs["conditioning"]
	}
}

// multiStageChains replaces the sampler by one SamplerCustomAdvanced for each part of the sampling. A part has its own text
// encoder (the prompt names the references it still has), negative start, ReferenceLatent chains and guider.
func multiStageChains(g localcfg.Graph, enc string, c refChains, uses []refUse, cuts []int, steps int, prompt string) error {
	run := g[c.sampler]
	rest := run.Inputs["sigmas"]
	g["mam_nonoise"] = localcfg.Node{ClassType: "DisableNoise", Inputs: map[string]json.RawMessage{}}

	latent, start := run.Inputs["latent_image"], 0
	bounds := append(slices.Clone(cuts), steps)
	for k, end := range bounds {
		active := activeAt(uses, start)
		encK := fmt.Sprintf("mam_enc_%d", k)
		clone := cloneNode(g[enc])
		clone.Inputs["text"] = Literal(imageWords(promptFor(active, prompt)))
		g[encK] = clone

		pos, neg := Link(encK, 0), Link(encK, 0)
		if c.negStart != enc {
			negK := fmt.Sprintf("mam_neg_%d", k)
			nclone := cloneNode(g[c.negStart])
			for name, raw := range nclone.Inputs {
				if to, _, ok := LinkOf(raw); ok && to == enc {
					nclone.Inputs[name] = Link(encK, 0)
				}
			}
			g[negK], neg = nclone, Link(negK, 0)
		}
		for i, u := range active {
			pos = chainNode(g, fmt.Sprintf("mam_pos_%d_%d", k, i), c.pos[u.slot], pos)
			neg = chainNode(g, fmt.Sprintf("mam_negref_%d_%d", k, i), c.neg[u.slot], neg)
		}

		part := rest
		if k < len(bounds)-1 { // cut the sigmas here: the first part is this stage, the second the rest
			split := fmt.Sprintf("mam_split_%d", k)
			g[split] = localcfg.Node{ClassType: "SplitSigmas", Inputs: map[string]json.RawMessage{"sigmas": rest, "step": Literal(end - start)}}
			part, rest = Link(split, 0), Link(split, 1)
		}
		guider, runK := fmt.Sprintf("mam_guider_%d", k), fmt.Sprintf("mam_run_%d", k)
		gc := cloneNode(g[c.guider])
		gc.Inputs["positive"], gc.Inputs["negative"] = pos, neg
		g[guider] = gc
		rc := cloneNode(run)
		rc.Inputs["guider"], rc.Inputs["sigmas"], rc.Inputs["latent_image"] = Link(guider, 0), part, latent
		if k > 0 {
			rc.Inputs["noise"] = Link("mam_nonoise", 0)
		}
		g[runK] = rc
		latent, start = Link(runK, 0), end
	}
	last, _, _ := LinkOf(latent)
	for _, n := range g {
		for name, raw := range n.Inputs {
			if to, idx, ok := LinkOf(raw); ok && to == c.sampler && idx == 0 {
				n.Inputs[name] = Link(last, 0)
			}
		}
	}
	for _, id := range slices.Concat([]string{c.sampler, c.guider, enc}, c.pos, c.neg) {
		delete(g, id)
	}
	if c.negStart != enc {
		delete(g, c.negStart)
	}
	return nil
}

// chainNode adds a copy of a ReferenceLatent node of the template that continues the conditioning from, and returns the link
// to it.
func chainNode(g localcfg.Graph, id, src string, from json.RawMessage) json.RawMessage {
	n := cloneNode(g[src])
	n.Inputs["conditioning"] = from
	g[id] = n
	return Link(id, 0)
}

// imageWords turns the <imageN> tags of a prompt into the "image N" a FLUX.2 prompt uses.
func imageWords(s string) string { return imageTag.ReplaceAllString(s, "image $1") }
