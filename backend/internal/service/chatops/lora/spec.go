// Package lora is the chat operation that takes a LoRA the user attaches to a message into the managed loras folder (#1140; spec/components/chat-settings-operations.md §4).
// The composer accepts a .safetensors file; the chat hands it to Offer (a chatops.FileOffer) instead of refusing it as an unsupported attachment. Offer streams the file
// to the importer the model manager screen uses (loraimport.Service.Stage: the same checks, the same imports folder) and answers with one confirmation card per file:
// what the file says of itself, and that the LoRA affects every Project. Nothing is placed in the loras folder before the user accepts: the yes button carries add_lora
// (Service.Commit with what the screen form would submit without edits) and "やめる" carries discard_lora, which throws the staged file away.
//
// Both operations exist only as the actions of those buttons: the model is never told about them (no Example, no Doc), so it can neither ask for nor invent them.
package lora

import (
	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
)

// The operations of this package.
const (
	// OpAdd adds the staged file to the loras folder (the yes button of the card).
	OpAdd commandinterpreter.OpType = "add_lora"
	// OpDiscard throws the staged file away (what declining the card carries out).
	OpDiscard commandinterpreter.OpType = "discard_lora"
)

// Args of both operations: the staged file that waits for the answer (loraimport.Staged.Token), and for OpAdd the name it is added under when the name it would get is
// taken by another file ("" keeps the name the file got when it was checked).
type Args struct {
	Token string `json:"token"`
	Name  string `json:"name,omitempty"`
}

func init() {
	spec := commandinterpreter.Spec{Clean: clean, AnyMode: true, Global: true}
	add := spec
	add.Confirm = true
	commandinterpreter.Register(OpAdd, add)
	commandinterpreter.Register(OpDiscard, spec)
}

// clean keeps an operation that names the staged file; the file itself is looked up when it is carried out.
func clean(op commandinterpreter.Operation) (commandinterpreter.Operation, bool) {
	var a Args
	if err := op.DecodeArgs(&a); err != nil || a.Token == "" {
		return commandinterpreter.Operation{}, false
	}
	return op, true
}
