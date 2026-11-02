package advform

import (
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
	adv "github.com/ousiassllc/mam/backend/internal/web/organisms/gentestadv"
)

// readExposed reads the fields "exposed.<node>.<input>" of a form: the first of the same name that holds a value (two models with the same input
// each have a field; the one of a model that is not ticked is disabled and posts nothing). An empty field is not given.
func readExposed(v url.Values) map[string]string {
	var out map[string]string
	for name, values := range v {
		key, ok := strings.CutPrefix(name, adv.FieldExposedPrefix)
		if !ok || key == "" {
			continue
		}
		for _, x := range values {
			if t := strings.TrimSpace(x); t != "" {
				if out == nil {
					out = map[string]string{}
				}
				out[key] = x // a text value is kept as typed (its spaces may matter); a number is trimmed by the service
				break
			}
		}
	}
	return out
}

// hiddenExposed carries the values of a request to the confirmation, in the order of the keys.
func hiddenExposed(values map[string]any) []view.Hidden {
	var h []view.Hidden
	for _, k := range slices.Sorted(maps.Keys(values)) {
		h = append(h, view.Hidden{Name: adv.FieldExposedPrefix + k, Value: text(values[k])})
	}
	return h
}

// text is a value of the request as the form holds it.
func text(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(x, 10)
	}
	return ""
}

// exposedGroups are the exposed inputs of the models that have some, with the values the form holds; a group is shown when its model is ticked.
func exposedGroups(f Fields, models []modelcatalog.Info, ticked []string) []adv.ExposedGroup {
	var out []adv.ExposedGroup
	for _, m := range models {
		if len(m.Exposed) == 0 {
			continue
		}
		g := adv.ExposedGroup{ModelID: m.ID, Label: m.Label, Shown: slices.Contains(ticked, m.ID)}
		for _, c := range m.Exposed {
			fl := adv.ExposedField{Key: c.Key, Label: c.Label, Type: string(c.Type), Value: f.Exposed[c.Key], Default: text(c.Default), Options: c.Options}
			if c.Min != nil && c.Max != nil {
				fl.Min, fl.Max = strconv.FormatFloat(*c.Min, 'f', -1, 64), strconv.FormatFloat(*c.Max, 'f', -1, 64)
			}
			g.Fields = append(g.Fields, fl)
		}
		out = append(out, g)
	}
	return out
}
