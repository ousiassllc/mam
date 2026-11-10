package commandinterpreter

import (
	"encoding/json"
	"fmt"
	"slices"
)

// Register adds an operation to the registry (ops.go). The operations that change the global settings (#1140) are one package each (service/chatops/...), which
// register their Spec when they are imported: the prompt, the guard on the model's word and the cleaning of its answer follow from the registry as for the
// budget. A type that is registered twice is a programming error.
func Register(t OpType, s Spec) {
	if _, dup := specs[t]; dup {
		panic(fmt.Sprintf("commandinterpreter: the operation %q is registered twice", t))
	}
	specs[t] = s
}

// NewOperation is an operation of type t that carries args (a struct of the operation's own package) as its Args.
func NewOperation(t OpType, args any) (Operation, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return Operation{}, fmt.Errorf("encode the arguments of %q: %w", t, err)
	}
	return Operation{Type: t, Args: raw}, nil
}

// DecodeArgs reads the Args of the operation into v (a pointer to the struct of its package). An operation without Args leaves v as it is.
func (o Operation) DecodeArgs(v any) error {
	if len(o.Args) == 0 {
		return nil
	}
	if err := json.Unmarshal(o.Args, v); err != nil {
		return fmt.Errorf("read the arguments of %q: %w", o.Type, err)
	}
	return nil
}

// Detected are the operations the message plainly asks for, read without the model (Spec.Detect), in the order of their types.
func Detected(message string) []Operation {
	var ops []Operation
	for _, t := range sortedTypes() {
		if detect := specs[t].Detect; detect != nil {
			ops = append(ops, detect(message)...)
		}
	}
	return ops
}

// WithDetected adds the operations the message plainly asks for (Detected) that the model did not return: the model's word wins for an operation of the same type, the rule only
// fills the gap (a slip of the model must not lose an API key card).
func WithDetected(ops []Operation, message string) []Operation {
	for _, d := range Detected(message) {
		if !slices.ContainsFunc(ops, func(op Operation) bool { return op.Type == d.Type }) {
			ops = append(ops, d)
		}
	}
	return ops
}

func sortedTypes() []OpType {
	types := make([]OpType, 0, len(specs))
	for t := range specs {
		types = append(types, t)
	}
	slices.Sort(types)
	return types
}

// A Refusal answers a message that asks for something the chat does not do on purpose (the program paths of ffmpeg and FaceFusion, the tokens of MCP: the issue's
// "意図的に GUI だけ"). It returns the sentence to tell and true for such a message.
type Refusal func(message string) (reply string, ok bool)

var refusals []Refusal

// RegisterRefusal adds a Refusal.
func RegisterRefusal(r Refusal) { refusals = append(refusals, r) }

// Refused is the reply for a message a Refusal catches; false for any other.
func Refused(message string) (string, bool) {
	for _, r := range refusals {
		if reply, ok := r(message); ok {
			return reply, true
		}
	}
	return "", false
}
