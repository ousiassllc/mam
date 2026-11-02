package comfyui

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// An editing workflow (localcfg.ModeEdit) hands the references to its text encoder, which reads them with a vision tower.
// A reference stays in the conditioning for the first part of the sampling only, and the shorter that part is the less the
// reference shows in the result: its strength s keeps it for the first s of the steps, so that 1 is the plain edit (the whole
// run), 0.7 (the default) the first 70% of it, and the rest of the run follows the prompt alone. Each reference has its own
// strength, so the sampling is cut at every distinct fraction and each part is encoded with the references still in it.

const (
	encodeClass = "TextEncodeQwenImage21"
	slotPrefix  = "images.image_"
	maxStages   = 6
)

var imageTag = regexp.MustCompile(`<image([0-9]+)>`)

// refUse is a reference of the run: its slot (the index of the assignment), role and the number of steps it stays in (the
// steps of the sampler when it stays to the end).
type refUse struct {
	slot  int
	role  string
	steps int
}

// stage rewrites an editing workflow for the references of the run. It returns an error for a graph that is not the shape
// the staging knows (a text encoder that takes the references and one KSampler fed by it).
func stage(g localcfg.Graph, wf localcfg.Workflow, v values) error {
	if wf.ReferenceMode != localcfg.ModeEdit || len(v.references) == 0 {
		return nil
	}
	enc, ok := nodeOf(g, wf, localcfg.ParamPrompt)
	if !ok || g[enc].ClassType != encodeClass {
		return fmt.Errorf("the editing workflow's prompt is not in a %s node", encodeClass)
	}
	sampler, ok := samplerOf(g, enc)
	if !ok {
		return fmt.Errorf("no KSampler takes the output of %s", enc)
	}
	total := intInput(g[sampler], "steps")
	uses := make([]refUse, len(v.references))
	for i := range v.references {
		o := v.opts.Ref(i)
		uses[i] = refUse{slot: i, role: o.Role, steps: stepsFor(o.StrengthOrDefault(), total)}
	}
	cuts := stageCuts(uses, total)
	snapToCuts(uses, cuts, total)
	if total <= 0 || len(cuts) == 0 {
		return singleStage(g, enc, uses, v.prompt)
	}
	return multiStage(g, enc, sampler, uses, cuts, v.prompt)
}

// stepsFor is how many of the steps a reference of the strength stays in: the strength times the steps, so that 1 is the whole
// run and the default 0.7 the first 70% of it. A reference that is asked for stays for one step at least.
func stepsFor(strength float64, total int) int {
	if strength >= 1 {
		return total
	}
	return min(total, max(1, int(math.Round(strength*float64(total)))))
}

// stageCuts are the steps at which a reference drops out of the sampling, ascending and distinct. A reference that stays to
// the end never drops out. With more distinct steps than maxStages-1 parts allow, the cuts kept are spread evenly over them
// (the first and the last included); snapToCuts moves the references of the others.
func stageCuts(uses []refUse, total int) []int {
	var cuts []int
	for _, u := range uses {
		if u.steps < total && !slices.Contains(cuts, u.steps) {
			cuts = append(cuts, u.steps)
		}
	}
	slices.Sort(cuts)
	keep := maxStages - 1
	if len(cuts) <= keep {
		return cuts
	}
	kept := make([]int, keep)
	for i := range kept {
		kept[i] = cuts[int(math.Round(float64(i*(len(cuts)-1))/float64(keep-1)))]
	}
	return kept
}

// snapToCuts moves every reference that drops out before the end (steps below total) to the nearest cut, so that a reference
// whose own cut was not kept does not stay to the end. A reference that stays to the end is left alone.
func snapToCuts(uses []refUse, cuts []int, total int) {
	for i, u := range uses {
		if u.steps >= total || len(cuts) == 0 {
			continue
		}
		uses[i].steps = slices.MinFunc(cuts, func(a, b int) int {
			return cmp.Or(cmp.Compare(absInt(a-u.steps), absInt(b-u.steps)), cmp.Compare(a, b))
		})
	}
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// singleStage keeps the node: every reference is in the whole run.
func singleStage(g localcfg.Graph, enc string, uses []refUse, prompt string) error {
	n := g[enc]
	n.Inputs["prompt"] = literal(promptFor(uses, prompt))
	setVAE(g, n, uses)
	return nil
}

// multiStage replaces the KSampler by one SamplerCustomAdvanced per part of the sampling, each with its own encoder node.
func multiStage(g localcfg.Graph, enc, sampler string, uses []refUse, cuts []int, prompt string) error {
	ks := g[sampler]
	steps := intInput(ks, "steps")
	sigmas := "mam_sigmas"
	g[sigmas] = localcfg.Node{ClassType: "BasicScheduler", Inputs: map[string]json.RawMessage{
		"model": ks.Inputs["model"], "scheduler": ks.Inputs["scheduler"], "steps": ks.Inputs["steps"], "denoise": ks.Inputs["denoise"],
	}}
	g["mam_select"] = localcfg.Node{ClassType: "KSamplerSelect", Inputs: map[string]json.RawMessage{"sampler_name": ks.Inputs["sampler_name"]}}
	g["mam_noise"] = localcfg.Node{ClassType: "RandomNoise", Inputs: map[string]json.RawMessage{"noise_seed": ks.Inputs["seed"]}}
	g["mam_nonoise"] = localcfg.Node{ClassType: "DisableNoise", Inputs: map[string]json.RawMessage{}}

	latent, rest, start := ks.Inputs["latent_image"], link(sigmas, 0), 0
	bounds := append(slices.Clone(cuts), steps)
	for k, end := range bounds {
		active := activeAt(uses, start)
		id := fmt.Sprintf("mam_enc_%d", k)
		clone := cloneNode(g[enc])
		clone.Inputs["prompt"] = literal(promptFor(active, prompt))
		setVAE(g, clone, active)
		keepSlots(clone, g[enc], active)
		g[id] = clone

		part := rest
		if k < len(bounds)-1 { // cut the sigmas here: the first part is this stage, the second the rest
			split := fmt.Sprintf("mam_split_%d", k)
			g[split] = localcfg.Node{ClassType: "SplitSigmas", Inputs: map[string]json.RawMessage{"sigmas": rest, "step": literal(end - start)}}
			part, rest = link(split, 0), link(split, 1)
		}
		guider, run := fmt.Sprintf("mam_guider_%d", k), fmt.Sprintf("mam_run_%d", k)
		g[guider] = localcfg.Node{ClassType: "CFGGuider", Inputs: map[string]json.RawMessage{
			"model": ks.Inputs["model"], "positive": link(id, 0), "negative": link(id, 1), "cfg": ks.Inputs["cfg"],
		}}
		noise := "mam_noise"
		if k > 0 {
			noise = "mam_nonoise"
		}
		g[run] = localcfg.Node{ClassType: "SamplerCustomAdvanced", Inputs: map[string]json.RawMessage{
			"noise": link(noise, 0), "guider": link(guider, 0), "sampler": link("mam_select", 0), "sigmas": part, "latent_image": latent,
		}}
		latent, start = link(run, 0), end
	}
	last, _, _ := linkOf(latent)
	for _, n := range g {
		for name, raw := range n.Inputs {
			if to, idx, ok := linkOf(raw); ok && to == sampler && idx == 0 {
				n.Inputs[name] = link(last, 0)
			}
		}
	}
	delete(g, sampler)
	delete(g, enc)
	return nil
}

// activeAt are the references still in the run from the step start on.
func activeAt(uses []refUse, start int) []refUse {
	var out []refUse
	for _, u := range uses {
		if u.steps > start {
			out = append(out, u)
		}
	}
	return out
}

// cloneNode copies a node so that the copy's inputs can change.
func cloneNode(n localcfg.Node) localcfg.Node {
	return localcfg.Node{ClassType: n.ClassType, Inputs: maps.Clone(n.Inputs)}
}

// keepSlots links the images of the references of the part into the encoder, in their order: the n-th is <image n> of its
// prompt. src is the encoder as the template has it, whose slot k holds the image of the reference k-1.
func keepSlots(n, src localcfg.Node, active []refUse) {
	for name := range n.Inputs {
		if strings.HasPrefix(name, slotPrefix) {
			delete(n.Inputs, name)
		}
	}
	for k, u := range active {
		n.Inputs[slotPrefix+strconv.Itoa(k+1)] = src.Inputs[slotPrefix+strconv.Itoa(u.slot+1)]
	}
}

// setVAE gives the encoder the VAE when a reference is to be reproduced (the whole picture or the one to edit): the encoder
// then also hands the picture's latents to the model. A face or a style is read with the vision tower alone. The VAE is the
// one the VAEDecode node takes (the lowest ID of them when there are several), else the VAELoader with the lowest ID.
func setVAE(g localcfg.Graph, n localcfg.Node, active []refUse) {
	if !slices.ContainsFunc(active, func(u refUse) bool { return u.role != localcfg.RoleFace && u.role != localcfg.RoleStyle }) {
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
		if to, _, ok := linkOf(g[id].Inputs["vae"]); ok && g[to].ClassType == "VAELoader" {
			n.Inputs["vae"] = link(to, 0)
			return
		}
	}
	for _, id := range ids {
		if g[id].ClassType == "VAELoader" {
			n.Inputs["vae"] = link(id, 0)
			return
		}
	}
}

// promptFor is the prompt of a part of the sampling: a sentence for each reference that tells the model what to take from
// it, then the user's prompt, whose <imageN> tags are renumbered to the references the part has (a reference that is gone
// is "the reference image").
func promptFor(active []refUse, prompt string) string {
	number := map[int]int{}
	for k, u := range active {
		number[u.slot+1] = k + 1
	}
	text := imageTag.ReplaceAllStringFunc(prompt, func(tag string) string {
		old, _ := strconv.Atoi(imageTag.FindStringSubmatch(tag)[1])
		if n, ok := number[old]; ok {
			return "<image" + strconv.Itoa(n) + ">"
		}
		return "the reference image"
	})
	var lines []string
	for k, u := range active {
		if s := roleSentence(u.role, k+1); s != "" {
			lines = append(lines, s)
		}
	}
	if text != "" {
		lines = append(lines, text)
	}
	return strings.Join(lines, "\n")
}

// roleSentence tells the editing model what to take from <image n>; the whole picture and the one to edit need no sentence.
func roleSentence(role string, n int) string {
	switch role {
	case localcfg.RoleFace:
		return fmt.Sprintf("Keep only the face and identity of <image%d>; its hair, clothes, pose and background do not matter: follow the description below for them.", n)
	case localcfg.RoleStyle:
		return fmt.Sprintf("Take only the art style, color grading and mood of <image%d>; its subject and composition do not matter.", n)
	}
	return ""
}

func nodeOf(g localcfg.Graph, wf localcfg.Workflow, p localcfg.Param) (string, bool) {
	for _, a := range wf.Assignments {
		if a.Param == p {
			_, ok := g[a.Node]
			return a.Node, ok
		}
	}
	return "", false
}

// samplerOf is the KSampler whose positive conditioning is the first output of the encoder.
func samplerOf(g localcfg.Graph, enc string) (string, bool) {
	ids := make([]string, 0, len(g))
	for id := range g {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, cmp.Compare[string])
	for _, id := range ids {
		if g[id].ClassType != "KSampler" {
			continue
		}
		if to, idx, ok := linkOf(g[id].Inputs["positive"]); ok && to == enc && idx == 0 {
			return id, true
		}
	}
	return "", false
}

func intInput(n localcfg.Node, name string) int {
	var v int
	if raw, ok := n.Inputs[name]; ok {
		_ = json.Unmarshal(raw, &v)
	}
	return v
}
