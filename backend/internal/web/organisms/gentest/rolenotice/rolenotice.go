// Package rolenotice is the modal that says, before a batch of the generation test runs, that a model uses a reference as a whole
// picture because it does not take its role (#959): what each model does with the references, and the two choices "戻る" and
// "このまま実行". It is a package of its own so that organisms/gentest stays inside its line limit.
package rolenotice

import (
	"github.com/a-h/templ"

	"github.com/ousiassllc/mam/backend/internal/web/molecules/modal"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Note is one model that does not take some roles of the references and so uses those pictures as a whole. Roles are the names of the
// roles as the role select shows them ("顔だけ").
type Note struct {
	Model string
	Roles []string
}

// Field is a value of the confirmation's form that the modal's run carries back unchanged.
type Field struct{ Name, Value string }

// Path is where the run button of a confirmation that has Notes asks for the modal (POST: the form of the confirmation).
func Path() string { return paths.GenerationTestPart("run-notice") }

// RunAttrs are the attributes of the run button of the confirmation: it submits the form when there is nothing to say (notes is 0),
// and otherwise opens the modal, from where the batch runs.
func RunAttrs(off bool, notes int) templ.Attributes {
	attrs := templ.Attributes{"disabled": off, "data-testid": "gt-run"}
	if notes == 0 {
		attrs["type"] = "submit"
		return attrs
	}
	for k, v := range modal.OpenAttrs("", "gt-run") {
		attrs[k] = v
	}
	delete(attrs, "hx-get")
	attrs["hx-post"] = Path()
	attrs["hx-disabled-elt"] = "this" // the form of the confirmation names its submit button, which this is not
	return attrs
}
