package judge

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// request builds the model call: instructions, the spec, then reference images and frames.
func (j *Judge) request(in Input, clip Clip, metrics domain.JudgeMetrics) llm.Request {
	req := llm.Request{System: systemPrompt, User: userPrompt(in, clip, metrics)}
	refs := in.References
	if n := j.cfg.Frames.MaxReferenceImages; len(refs) > n {
		refs = refs[:n]
	}
	for _, r := range refs {
		caption := "Reference image (" + string(r.Kind) + ")"
		if r.Name != "" {
			caption += ": " + r.Name
		}
		req.Images = append(req.Images, llm.Image{MimeType: r.MimeType, Data: r.Data, Caption: caption})
	}
	for i, f := range evenly(clip.Frames, j.cfg.Frames.ReviewFrames) {
		req.Images = append(req.Images, llm.Image{
			MimeType: "image/jpeg", Data: f.JPEG,
			Caption: fmt.Sprintf("Video frame %d of the Take at t=%.2fs (source frame %d)", i+1, f.Time, f.Number),
		})
	}
	return req
}

func userPrompt(in Input, clip Clip, metrics domain.JudgeMetrics) string {
	var b strings.Builder
	spec, _ := json.MarshalIndent(in.Spec, "", "  ")
	b.WriteString("Judge the generated video Take of this shot.\n\nCANONICAL SHOT SPEC (what the shot must show):\n")
	b.Write(spec)
	for _, n := range []struct {
		label string
		spec  *domain.ShotSpec
	}{{"PREVIOUS SHOT SPEC (the Take must connect to it)", in.Previous}, {"NEXT SHOT SPEC (the Take must connect to it)", in.Next}} {
		if n.spec != nil {
			raw, _ := json.Marshal(n.spec)
			fmt.Fprintf(&b, "\n\n%s:\n%s", n.label, raw)
		}
	}
	if locked := lockedNames(in.Locked); len(locked) > 0 {
		fmt.Fprintf(&b, "\n\nLOCKED dimensions (the user forbids any change; any visible deviation is a lock violation): %s", strings.Join(locked, ", "))
	}
	fmt.Fprintf(&b, "\n\nThe Take is %.1f seconds long at %.1f fps. The frames below are sampled evenly; times are seconds from the start.", clip.Duration, clip.FPS)
	if hasEvaluated(metrics) {
		b.WriteString("\nThe face identity of the Character is measured separately by a numeric model: do NOT report face_identity_drift yourself; still judge the face qualitatively (naturalness, distortion, expression).")
	}
	if axes := in.Template.Axes(); len(axes) > 0 {
		names := make([]string, len(axes))
		for i, a := range axes {
			names[i] = string(a)
		}
		fmt.Fprintf(&b, "\nAlso give \"scores\": a number from 0 to 100 for each of these axes of the %s template, in priority order: %s.", in.Template.Label(), strings.Join(names, ", "))
	}
	if section := in.Brand.PromptSection(); section != "" {
		b.WriteString("\n\nBRAND / SAFETY CONSTRAINTS of this project (check the frames against every one; text shown on screen counts):\n" + section)
		b.WriteString("For each broken constraint report an issue with \"type\": \"" + domain.BrandIssueType + "\" and \"brand_constraint_id\" set to its id. A LOCKED constraint that is broken fails the Take whatever else is good; an ADVISORY one is only a warning.")
	}
	b.WriteString("\nReference images (if any) come first, then the video frames.")
	return b.String()
}

func lockedNames(locked map[domain.JudgeDimension]bool) []string {
	var out []string
	for _, d := range domain.JudgeDimensions() {
		if locked[d] {
			out = append(out, string(d))
		}
	}
	return out
}

var systemPrompt = buildSystemPrompt()

func buildSystemPrompt() string {
	dims := make([]string, 0, len(domain.JudgeDimensions()))
	for _, d := range domain.JudgeDimensions() {
		dims = append(dims, string(d))
	}
	actions := make([]string, 0, len(domain.RepairActions()))
	for _, a := range domain.RepairActions() {
		actions = append(actions, string(a))
	}
	return `You are the AI Video Judge of an AI video production team.
You are shown frames of ONE generated video Take, sometimes reference images, and the shot's Canonical Shot Spec.
Decide whether the Take can be used. Do not give only a score: point at the time ranges that are wrong and say how to repair them.

Evaluate these dimensions (use these exact identifiers in "dimension"): ` + strings.Join(dims, ", ") + `.

Return ONLY one JSON object:
{
  "result": "pass" or "fail",
  "overall_confidence": a number from 0 to 1 (how sure you are of the result),
  "issues": [
    {"type": "snake_case_identifier such as product_logo_deformation or camera_speed",
     "severity": "low" | "medium" | "high",
     "dimension": "one of the dimension identifiers",
     "time_range": "2.1-2.8s" (seconds from the start of the Take; omit if it concerns the whole Take),
     "brand_constraint_id": "the id of the broken Brand / Safety Constraint, only for a constraint listed in the request",
     "description": "one short English sentence"}
  ],
  "scores": {"axis_identifier": a number from 0 to 100, ...} (only the axes named in the request; omit when none are named),
  "preserve": ["what is good and must be kept when repairing, e.g. character, acting, background"],
  "repair_strategy": {"action": one of ` + strings.Join(actions, ", ") + `,
                      "instructions": ["snake_case_instruction such as reduce_camera_speed_40_percent"]}
}

Rules:
- severity "high" means the Take cannot be used as it is; "medium" is a visible flaw; "low" is minor.
- If a LOCKED dimension is listed, any visible deviation of it is at least "medium".
- Report only what you can see in the frames; do not invent issues. An empty "issues" list is fine for a good Take.
- Use "partial_regeneration" when only some time ranges are wrong and "full_retake" when the whole Take is wrong.
- Write English only, except for names copied from the input.`
}
