// Package loraselect is the choice of a LoRA in the form of the generation test (#1119, #1011): the field of the model picker (molecules/modelpick, source "lora") that
// chooses one LoRA file of the engine, and the note under it that says what is known of the chosen file (the base model it is made for, the trigger words, the
// license) and warns when it does not fit the model that is ticked. The note is drawn by static/src/components/gentest-lora/lora-note.ts from the values the field's
// hidden inputs hold (modelcards.LoRAKey*) and from the data attributes of the model cards, so that it follows the choice without a request.
package loraselect
