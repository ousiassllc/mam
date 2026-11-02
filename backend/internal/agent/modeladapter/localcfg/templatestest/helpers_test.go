package templatestest

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

func graphOf(t *testing.T, id string) localcfg.Graph {
	t.Helper()
	tpl, ok := localcfg.TemplateByID(id)
	require.True(t, ok, id)
	g, err := localcfg.ParseGraph(tpl.Model.Workflow.Graph)
	require.NoError(t, err)
	return g
}

func idsOf(g localcfg.Graph, class string) []string {
	var ids []string
	for id, n := range g {
		if n.ClassType == class {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// linked returns the node that feeds the input of a node (a link), for the wiring checks.
func linked(t *testing.T, g localcfg.Graph, id, input string) (string, localcfg.Node) {
	t.Helper()
	var link []json.RawMessage
	require.NoError(t, json.Unmarshal(g[id].Inputs[input], &link), "node %s input %s is a link", id, input)
	var from string
	require.NoError(t, json.Unmarshal(link[0], &from))
	return from, g[from]
}
