package install

import (
	"strings"

	"github.com/ousiassllc/mam/backend/internal/service/chatops/nlu"
)

// named is something the director names by its id or its name: a model of the catalog, a runtime, a file of a repository.
type named struct{ ID, Name string }

// resolve finds what the director named among items, by the most exact reading that finds anything: the id, the name, then a name or id that contains what was said.
// Several matches are all returned (the chat asks which); none is nil. It never returns something that does not match: a name the model made up finds nothing.
func resolve(query string, items []named) []named {
	q := nlu.Norm(query)
	if q == "" {
		return nil
	}
	tiers := [...]func(named) bool{
		func(n named) bool { return strings.EqualFold(strings.TrimSpace(query), n.ID) },
		func(n named) bool { return nlu.Norm(n.ID) == q || nlu.Norm(n.Name) == q },
		func(n named) bool {
			return strings.Contains(nlu.Norm(n.Name), q) || strings.Contains(nlu.Norm(n.ID), q)
		},
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
