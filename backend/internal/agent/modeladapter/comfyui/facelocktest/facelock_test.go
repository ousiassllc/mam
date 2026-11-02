// Package facelocktest tests what the "face only" role of an editing workflow does to the graph and to the prompt (Issue #960) through
// the ComfyUI backend against the fake server, for the two editing templates that share the staging (Qwen-Image 2.1 and
// Qwen-Image-Edit 2511). It is a package of its own because the directory of the backend's tests is full (linterly).
//
// What is pinned is the behaviour, not the wording: the face role (and only it) has a longer instruction and a longer stay in the
// sampling, the references keep their numbers, and the other roles are what they were.
package facelocktest_test

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
)

// The editing templates and how each calls the reference of a slot in the prompt it encodes (the EditPlus encoder numbers its pictures
// "Picture N", the other reads <imageN> as it is).
var templates = []struct {
	id    string
	named func(n string) string
}{
	{"txt2img-qwen-image21", func(n string) string { return "<image" + n + ">" }},
	{"txt2img-qwen-image-edit-2511", func(n string) string { return "Picture " + n }},
}

type submitted = map[string]struct {
	ClassType string                     `json:"class_type"`
	Inputs    map[string]json.RawMessage `json:"inputs"`
}

func backend(t *testing.T, f *comfyuitest.Fake, tpl string) *comfyui.Backend {
	t.Helper()
	tpl2, ok := localcfg.TemplateByID(tpl)
	require.True(t, ok)
	c, err := localhttp.New("box", localcfg.Endpoint{Kind: localcfg.KindComfyUI, BaseURL: f.URL()}, credentialtest.New(), t.TempDir())
	require.NoError(t, err)
	c.PollEvery = 10 * time.Millisecond
	return &comfyui.Backend{Client: c, Model: tpl2.Model}
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	return c
}

func str(raw json.RawMessage) string  { var s string; _ = json.Unmarshal(raw, &s); return s }
func num(raw json.RawMessage) float64 { var n float64; _ = json.Unmarshal(raw, &n); return n }

// refs is a request with one input image for each option.
func refs(prompt string, opts ...localcfg.RefOption) modeladapter.ImageRequest {
	req := modeladapter.ImageRequest{Prompt: prompt, AspectRatio: "1:1", Options: localcfg.ImageOptions{Refs: opts}}
	for range opts {
		req.Inputs = append(req.Inputs, modeladapter.Input{Data: comfyuitest.PNG, MimeType: "image/png"})
	}
	return req
}

// run sends the request and returns the graph the server got.
func run(t *testing.T, tpl string, req modeladapter.ImageRequest) submitted {
	t.Helper()
	f := comfyuitest.New(t)
	_, err := backend(t, f, tpl).GenerateImage(ctx(t), "", req)
	require.NoError(t, err)
	return f.Graph()
}

// promptOf is the positive prompt the sampling is given, whether the run is one part or several (the first part when staged).
func promptOf(g submitted) string {
	if n, ok := g["mam_enc_0"]; ok {
		return str(n.Inputs["prompt"])
	}
	return str(g["6"].Inputs["prompt"])
}

// samePerson is a phrase of the instruction that only the face role has: it asks that the result be the very person of the picture.
var samePerson = regexp.MustCompile(`(?i)same person`)

// Only a face reference is given the instruction that names the face; every other role gets what it had, and a run without a
// reference gets the prompt as it is.
func TestOnlyTheFaceRoleChangesTheInstruction(t *testing.T) {
	for _, tpl := range templates {
		t.Run(tpl.id, func(t *testing.T) {
			face := promptOf(run(t, tpl.id, refs("a woman on a beach", localcfg.RefOption{Role: localcfg.RoleFace, Strength: 1})))
			assert.Regexp(t, samePerson, face)
			assert.Contains(t, face, "a woman on a beach", "the user's prompt is still there")
			assert.Contains(t, face, tpl.named("1"), "the instruction names the picture")
			assert.Greater(t, len(face), len("a woman on a beach")+len("Keep only the face and identity of <image1>"), "it says more than the one general sentence")

			for _, role := range []string{localcfg.RoleWhole, localcfg.RoleEdit, ""} {
				assert.Equal(t, "a woman on a beach", promptOf(run(t, tpl.id, refs("a woman on a beach", localcfg.RefOption{Role: role, Strength: 1}))), "role %q needs no instruction", role)
			}
			style := promptOf(run(t, tpl.id, refs("a woman on a beach", localcfg.RefOption{Role: localcfg.RoleStyle, Strength: 1})))
			assert.NotRegexp(t, samePerson, style)
			assert.Contains(t, style, "art style")
			assert.Equal(t, "a cat", promptOf(run(t, tpl.id, refs("a cat"))))
		})
	}
}

// With several references the instruction of the face is about the number the face has in the encoder, whichever place it is at,
// and the numbering of the other pictures is not disturbed.
func TestFaceInstructionFollowsTheNumberOfTheFace(t *testing.T) {
	for _, tpl := range templates {
		t.Run(tpl.id, func(t *testing.T) {
			g := run(t, tpl.id, refs("<image1> in the pose of <image2>",
				localcfg.RefOption{Role: localcfg.RoleEdit, Strength: 1},
				localcfg.RefOption{Role: localcfg.RoleFace, Strength: 1}))
			p := promptOf(g)
			assert.Contains(t, p, "Keep only the face and identity of "+tpl.named("2"), "the face is the second picture")
			assert.NotContains(t, p, "identity of "+tpl.named("1"))
			assert.Contains(t, p, tpl.named("1")+" in the pose of "+tpl.named("2"), "the tags of the user's prompt are the numbers of the encoder")
			// every picture the instruction names is one that the encoder has
			for _, m := range regexp.MustCompile(`(?:<image|Picture )(\d+)`).FindAllStringSubmatch(p, -1) {
				assert.Contains(t, []string{"1", "2"}, m[1], p)
			}
		})
	}
}

// A face that leaves the sampling before another reference does is not mentioned in the later parts, and the picture that stays
// is renumbered to the first.
func TestFaceInstructionLeavesWithTheFace(t *testing.T) {
	for _, tpl := range templates {
		t.Run(tpl.id, func(t *testing.T) {
			g := run(t, tpl.id, refs("x", localcfg.RefOption{Role: localcfg.RoleFace, Strength: 0.5}, localcfg.RefOption{Role: localcfg.RoleStyle, Strength: 1}))
			assert.Regexp(t, samePerson, str(g["mam_enc_0"].Inputs["prompt"]))
			later := str(g["mam_enc_1"].Inputs["prompt"])
			assert.NotRegexp(t, samePerson, later, "the face is gone from the second part")
			assert.Contains(t, later, "mood of "+tpl.named("1"), "the style picture is the first of the second part")
		})
	}
}

// A face that names no strength stays in the sampling longer than the whole picture does, and a strength that is named is kept
// as it is.
func TestFaceStaysLongerThanTheWholePictureByDefault(t *testing.T) {
	for _, tpl := range templates {
		t.Run(tpl.id, func(t *testing.T) {
			// A second reference of full strength makes the stay of the first show as the step the sampling is cut at.
			cut := func(role string, strength float64) float64 {
				g := run(t, tpl.id, refs("x", localcfg.RefOption{Role: role, Strength: strength}, localcfg.RefOption{Role: localcfg.RoleEdit, Strength: 1}))
				return num(g["mam_split_0"].Inputs["step"])
			}
			face, whole := cut(localcfg.RoleFace, 0), cut(localcfg.RoleWhole, 0)
			assert.Greater(t, face, whole)
			assert.Equal(t, whole, cut(localcfg.RoleStyle, 0), "the other roles keep their default")
			assert.Equal(t, whole, cut(localcfg.RoleFace, localcfg.DefaultReferenceStrength), "a strength the user named is not raised")
		})
	}
}
