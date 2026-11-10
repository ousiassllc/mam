package install

import (
	"strings"
	"unicode"
)

// named is something the director names by its id or its name: a model of the catalog, a runtime, a file of a repository.
type named struct{ ID, Name string }

// key writes a name the way two spellings of it meet: lower case, without blanks or punctuation ("FLUX.1 schnell" and "flux1-schnell" are "flux1schnell").
func key(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// resolve finds what the director named among items, by the most exact reading that finds anything: the id, the name, then a name or id that contains what was said.
// Several matches are all returned (the chat asks which); none is nil. It never returns something that does not match: a name the model made up finds nothing.
func resolve(query string, items []named) []named {
	q := key(query)
	if q == "" {
		return nil
	}
	tiers := [...]func(named) bool{
		func(n named) bool { return strings.EqualFold(strings.TrimSpace(query), n.ID) },
		func(n named) bool { return key(n.ID) == q || key(n.Name) == q },
		func(n named) bool { return strings.Contains(key(n.Name), q) || strings.Contains(key(n.ID), q) },
	}
	for _, match := range tiers {
		var found []named
		for _, n := range items {
			if match(n) {
				found = append(found, n)
			}
		}
		if len(found) > 0 {
			return found
		}
	}
	return nil
}

// labels are the buttons of the candidates: the name, with the id beside it when two candidates share the name.
func labels(found []named) []string {
	count := map[string]int{}
	for _, n := range found {
		count[n.Name]++
	}
	out := make([]string, len(found))
	for i, n := range found {
		out[i] = n.Name
		if count[n.Name] > 1 {
			out[i] += "（" + n.ID + "）"
		}
	}
	return out
}

// names are the names of the candidates, for a sentence.
func names(found []named) []string {
	out := make([]string, len(found))
	for i, n := range found {
		out[i] = n.Name
	}
	return out
}
