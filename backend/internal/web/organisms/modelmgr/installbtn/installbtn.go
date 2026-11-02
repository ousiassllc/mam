// Package installbtn builds the attributes of the install button of a model card (htmx), apart from the model manager organism whose directory is full (linterly).
package installbtn

import "github.com/a-h/templ"

// Attrs are the attributes of the install button: it posts to url and swaps target. A model this PC is outside the OS / GPU generation of (warning,
// Issue #979) asks first with the reason; the answer yes installs it (a warning never refuses).
func Attrs(url, target, name, warning string) templ.Attributes {
	attrs := templ.Attributes{"type": "button", "hx-post": url, "hx-target": target, "hx-swap": "outerHTML", "hx-sync": "this:drop", "hx-disabled-elt": "this", "data-testid": "mm-install"}
	if warning != "" {
		attrs["hx-confirm"] = warning + "\n\nそれでも「" + name + "」を導入しますか？"
	}
	return attrs
}
