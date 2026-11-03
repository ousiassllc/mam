// Package launchargs is how the arguments of a runtime's server are made: the catalog writes them once, with placeholders ("{port}", "{model}", "{choice}"
// and the values the model in force gives, choice.Choice.Values), and every start of the server replaces them. A runtime whose server takes a model file
// and settings (llama.cpp's llama-server: -m, --ctx-size, -ngl) is the case: the user chooses a model, never an argument, and the only command lines
// that can run are the fixed combinations the catalog holds (spec/architecture/model-manager/local-runtime.md §17). It is a package of its own because
// the directories of the catalog and the installer are at their line budget; the catalog validates with Check, the installer starts with Expand.
package launchargs

import (
	"fmt"
	"maps"
	"regexp"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/choice"
)

// The placeholders every start fills in. Any other one is a key of the Values of the model in force.
const (
	Port   = "port"   // the port the server listens on
	Model  = "model"  // the file of the model in force (the asset of the choice with the role "model"), relative to the environment directory
	Choice = "choice" // the id of the model in force
)

// RoleModel is the Resource.Role of the asset that is the file the server loads.
const RoleModel = "model"

var placeholder = regexp.MustCompile(`\{([a-z][a-z0-9_]*)\}`)

// Names are the placeholders the arguments use, each once, in the order they first appear.
func Names(args []string) []string {
	var names []string
	for _, a := range args {
		for _, m := range placeholder.FindAllStringSubmatch(a, -1) {
			if !slices.Contains(names, m[1]) {
				names = append(names, m[1])
			}
		}
	}
	return names
}

// Expand replaces the placeholders of args with values. A placeholder that has no value is an error: a server is never started with "{ctx_size}" as an argument.
func Expand(args []string, values map[string]string) ([]string, error) {
	out := make([]string, len(args))
	for n, a := range args {
		var missing string
		out[n] = placeholder.ReplaceAllStringFunc(a, func(m string) string {
			v, ok := values[m[1:len(m)-1]]
			if !ok {
				missing = m
			}
			return v
		})
		if missing != "" {
			return nil, fmt.Errorf("launch: no value for %s", missing)
		}
	}
	return out, nil
}

// Check validates that the arguments of the runtime named where and the models it can run with go together: every placeholder is one of the built-in
// ones or a value that every model gives, and a runtime whose arguments name the "{model}" has exactly one asset with the role "model" in each model
// (models says how many a model has).
func Check(where string, args []string, choices []choice.Choice, models func(choiceID string) int) []error {
	var errs []error
	for _, name := range Names(args) {
		switch name {
		case Port:
		case Model:
			for _, c := range choices {
				if n := models(c.ID); n != 1 {
					errs = append(errs, fmt.Errorf("%s choice %s: the launch args run {model}: the model needs exactly one asset with the role %q, has %d", where, c.ID, RoleModel, n))
				}
			}
			if len(choices) == 0 {
				errs = append(errs, fmt.Errorf("%s: the launch args run {model} but the runtime has no models to choose", where))
			}
		case Choice:
			if len(choices) == 0 {
				errs = append(errs, fmt.Errorf("%s: the launch args run {choice} but the runtime has no models to choose", where))
			}
		default:
			for _, c := range choices {
				if _, ok := c.Values[name]; !ok {
					errs = append(errs, fmt.Errorf("%s choice %s: the launch args use {%s}, which the model gives no value for", where, c.ID, name))
				}
			}
			if len(choices) == 0 {
				errs = append(errs, fmt.Errorf("%s: the launch args use {%s} but the runtime has no models to give it", where, name))
			}
		}
	}
	return errs
}

// Values are the values of a start: what the model in force gives, and the port, the model file and the id of the model (the built-in ones win; a
// model cannot give them, choice.Check).
func Values(port int, modelFile, choiceID string, given map[string]string) map[string]string {
	v := maps.Clone(given)
	if v == nil {
		v = map[string]string{}
	}
	v[Port] = fmt.Sprint(port)
	if choiceID != "" {
		v[Choice] = choiceID
	}
	if modelFile != "" {
		v[Model] = modelFile
	}
	return v
}
