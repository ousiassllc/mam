// Package commandinterpreter is the Command Interpreter (spec/spec.md §24): it turns the natural
// language direction of the user into structured operations (LOCK settings, parameter updates that
// regenerate one Shot while preserving the rest, a mode switch), and resolves which Shot the
// direction is about when the chat did not say so. It only interprets: the service executes the
// operations, and LOCK is judged there, on the data being written.
package commandinterpreter

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Interpreter is the Command Interpreter Agent.
type Interpreter struct{ llm llm.Completer }

// New returns an Interpreter that reasons through c.
func New(c llm.Completer) *Interpreter { return &Interpreter{llm: c} }

// ShotOption is a Shot the direction may be about, numbered as the user sees it (1-based).
type ShotOption struct {
	Scene, Shot int
	Objective   string
}

// Input is what the Command Interpreter works from (spec/components/overview.md §1).
type Input struct {
	Message string
	// Shots lists every Shot of the Project in story order.
	Shots []ShotOption
	// Target is the Shot the direction is about when the chat already fixed it (a reply to its
	// card, or the latest card of the thread); nil when the Interpreter has to resolve it.
	Target *ShotOption
	// Spec is the Canonical Shot Spec of Target, when it has one.
	Spec *domain.ShotSpec
	// Locks is the setting of the parameters of Target.
	Locks domain.ParameterLocks
}

// OpType is the kind of a structured operation.
type OpType string

// Operation types.
const (
	// OpSetLock sets a parameter of the target Shot (or Story / Music of the Project) to AUTO,
	// MANUAL or LOCK (F-05): "この顔は好きだから固定" is LOCK on Character Identity.
	OpSetLock OpType = "set_lock"
	// OpRegenerate changes some parts of the target Shot and regenerates that Shot only; the
	// other parts are preserved (UC-2): "カメラ動かしすぎ" changes the camera, keeps character,
	// acting and environment.
	OpRegenerate OpType = "regenerate"
	// OpSetMode switches AUTO / DIRECTOR.
	OpSetMode OpType = "set_mode"
	// OpCreateBranch makes Branches at the target Shot (F-17, UC-5): "このカットから別展開を3つ作って"
	// is three Branches, each a different continuation.
	OpCreateBranch OpType = "create_branch"
	// OpUnsupported is a direction that has no operation (exporting, editing the timeline).
	OpUnsupported OpType = "unsupported"
)

// Operation is one structured operation. Only the fields of its Type are set.
type Operation struct {
	Type OpType `json:"type"`

	// OpSetLock. Value is the Video Model (a domain.ModelName) fixed with MANUAL or LOCK.
	Parameter domain.LockParameter `json:"parameter,omitempty"`
	Mode      domain.LockMode      `json:"mode,omitempty"`
	Value     string               `json:"value,omitempty"`

	// OpRegenerate. Changes are the parts of the Shot Spec the direction changes; Preserve are the
	// parts that MUST stay as they are (always the complement of Changes, plus what the user
	// named). Instruction is the direction for the Prompt Planner.
	Changes     []domain.SpecPart `json:"changes,omitempty"`
	Preserve    []domain.SpecPart `json:"preserve,omitempty"`
	Instruction string            `json:"instruction,omitempty"`

	// OpSetMode.
	ProjectMode domain.Mode `json:"project_mode,omitempty"`

	// OpCreateBranch. Count is how many Branches (1 to domain.MaxBranchesPerRequest), PointType
	// where they start (scene or shot) and Directions the developments the director named, one per
	// Branch; the team proposes the developments the director left open.
	Count      int                    `json:"count,omitempty"`
	PointType  domain.BranchPointType `json:"point_type,omitempty"`
	Directions []string               `json:"directions,omitempty"`

	// OpUnsupported: why, in the user's language.
	Reason string `json:"reason,omitempty"`
}

// Command is the interpretation of one message.
type Command struct {
	// Scene and Shot name the Shot the direction is about (1-based); 0 when it did not say.
	Scene, Shot int
	// Question is what to ask the user when the direction cannot be carried out as it stands
	// (which Shot?), in the user's language. When set, Operations is empty.
	Question   string
	Operations []Operation
}

const system = `You are the Command Interpreter of an AI video production team. The director talks to the team in chat.
Convert the director's message into structured operations. Return ONLY one JSON object:
{
  "scene": 2, "shot": 1,
  "question": "",
  "operations": [
    {"type": "set_lock", "parameter": "character_identity", "mode": "lock"},
    {"type": "set_lock", "parameter": "video_model", "mode": "manual", "value": "kling-3.0"},
    {"type": "regenerate", "changes": ["camera"], "preserve": [], "instruction": "camera movement: static, no handheld motion"},
    {"type": "set_mode", "project_mode": "auto"},
    {"type": "create_branch", "count": 3, "point_type": "shot", "directions": ["ドアを開ける", "逃げる"]},
    {"type": "unsupported", "reason": "書き出しはこのチャットからはまだ指示できません"}
  ]
}

Operations:
- "set_lock": set one parameter to "auto" (the AI decides), "manual" (the user specified it) or "lock" (freeze the current value, no change by the AI). Parameters: ` + "`story`, `music`" + ` (the whole project) and, for one shot: ` + "`dialogue`, `character_identity`, `acting`, `camera`, `lighting`, `product_geometry`, `prompt`, `video_model`, `take_selection`, `editing`" + `. "Fix / keep / do not change X" (固定・変えないで) is "lock". "value" is only for video_model: "kling-3.0" or "seedance-2.0".
- "regenerate": the director wants this shot changed. "changes" lists the parts that change: ` + "`character`, `product`, `location`, `acting`, `camera`, `lighting`" + ` ("location" is the environment). "preserve" lists parts the director explicitly asks to keep although the change might touch them; leave it empty otherwise, everything not in "changes" is preserved anyway. "instruction" is one or two English sentences directing the change, without restating what stays.
- "set_mode": "auto" or "director": the director wants the team to lead again (auto) or to only change what is told (director).
- "create_branch": the director wants alternative developments of the story from this shot or scene ("このカットから別展開を3つ作って"). "count" is how many (default 1). "point_type" is "shot" (after this shot, the default) or "scene" (from the start of this shot's scene). "directions" lists the developments the director named, one per branch, in the director's words; leave it empty when the director named none, the team proposes them.
- "unsupported": the director asks for something that is not available: exporting, editing the timeline. "reason" is one short Japanese sentence.

Rules:
- "scene" and "shot" are the numbers the director names ("Scene 2 の Shot 1", "シーン2のショット1"). Use 0 for both when the message does not name a shot. When the input says which shot the message is about, use its numbers.
- If the message is about a shot but it is impossible to tell which one (no shot named, none given as the target, several shots exist), set "question" to one short Japanese question asking which shot, and return no operations.
- Several operations may be returned for one message ("カメラは固定して、表情だけ変えて" = lock camera + regenerate with changes ["acting"]).
- A message that is only a question, a greeting or an opinion has no operation: return an empty "operations" and an empty "question".
- Never invent operations the message does not ask for.`

// Interpret converts the message into a Command. An answer that is not the JSON above is an
// error wrapping llm.ErrInvalidOutput. Operations the message cannot mean (an unknown parameter or
// mode) are left out.
func (i *Interpreter) Interpret(ctx context.Context, in Input) (Command, error) {
	type raw struct {
		Scene      int         `json:"scene"`
		Shot       int         `json:"shot"`
		Question   string      `json:"question"`
		Operations []Operation `json:"operations"`
	}
	out, err := llm.CompleteJSON[raw](ctx, i.llm, llm.AgentCommandInterpreter, llm.Request{System: system, User: userPrompt(in)})
	if err != nil {
		return Command{}, fmt.Errorf("command interpreter: %w", err)
	}
	cmd := Command{Scene: max(out.Scene, 0), Shot: max(out.Shot, 0), Question: strings.TrimSpace(out.Question)}
	if cmd.Question != "" {
		return cmd, nil
	}
	for _, op := range out.Operations {
		if op, ok := clean(op, in.Message); ok {
			cmd.Operations = append(cmd.Operations, op)
		}
	}
	return cmd, nil
}

// clean validates an operation and fills in what follows from it.
func clean(op Operation, message string) (Operation, bool) {
	switch op.Type {
	case OpSetLock:
		if !op.Mode.Valid() || (!op.Parameter.ValidForShot() && !op.Parameter.ValidForProject()) {
			return Operation{}, false
		}
		if op.Parameter != domain.ParamVideoModel || !slices.Contains(domain.VideoModels(), domain.ModelName(op.Value)) {
			op.Value = ""
		}
		return Operation{Type: OpSetLock, Parameter: op.Parameter, Mode: op.Mode, Value: op.Value}, true
	case OpRegenerate:
		changes := validParts(op.Changes)
		preserve := validParts(op.Preserve)
		// What the direction does not change stays: Character, Acting and Environment are kept
		// while the camera moves (UC-2).
		for _, p := range domain.SpecParts() {
			if len(changes) > 0 && !slices.Contains(changes, p) && !slices.Contains(preserve, p) {
				preserve = append(preserve, p)
			}
		}
		preserve = slices.DeleteFunc(preserve, func(p domain.SpecPart) bool { return slices.Contains(changes, p) })
		instruction := strings.TrimSpace(op.Instruction)
		if instruction == "" {
			instruction = strings.TrimSpace(message)
		}
		return Operation{Type: OpRegenerate, Changes: changes, Preserve: preserve, Instruction: instruction}, true
	case OpSetMode:
		if !op.ProjectMode.Valid() {
			return Operation{}, false
		}
		return Operation{Type: OpSetMode, ProjectMode: op.ProjectMode}, true
	case OpCreateBranch:
		count := min(max(op.Count, 1), domain.MaxBranchesPerRequest)
		typ := op.PointType
		if typ != domain.BranchAtScene {
			typ = domain.BranchAtShot // a frame needs an offset a message cannot give
		}
		var directions []string
		for _, d := range op.Directions {
			if d = strings.TrimSpace(d); d != "" && len(directions) < count {
				directions = append(directions, d)
			}
		}
		return Operation{Type: OpCreateBranch, Count: count, PointType: typ, Directions: directions}, true
	case OpUnsupported:
		return Operation{Type: OpUnsupported, Reason: strings.TrimSpace(op.Reason)}, true
	}
	return Operation{}, false
}

func validParts(in []domain.SpecPart) []domain.SpecPart {
	var out []domain.SpecPart
	for _, p := range in {
		if p.Valid() && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

func userPrompt(in Input) string {
	var b strings.Builder
	b.WriteString("## Message of the director\n" + in.Message + "\n\n## Shots of the project\n")
	if len(in.Shots) == 0 {
		b.WriteString("(none yet)\n")
	}
	for _, s := range in.Shots {
		fmt.Fprintf(&b, "- Scene %d / Shot %d: %s\n", s.Scene, s.Shot, s.Objective)
	}
	if in.Target != nil {
		fmt.Fprintf(&b, "\n## The message is about Scene %d / Shot %d\n", in.Target.Scene, in.Target.Shot)
		if in.Spec != nil {
			spec, _ := json.Marshal(in.Spec)
			b.WriteString("Current Canonical Shot Spec: " + string(spec) + "\n")
		}
		var settings []string
		for _, p := range domain.ShotLockParameters() {
			if m := in.Locks.Mode(p); m != domain.LockAuto {
				settings = append(settings, string(p)+"="+string(m))
			}
		}
		if len(settings) > 0 {
			b.WriteString("Parameter settings: " + strings.Join(settings, ", ") + "\n")
		}
	}
	return b.String()
}
