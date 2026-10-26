package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
)

// Warning is something the connected ComfyUI lacks for a workflow. It never blocks saving or running (the server
// may get the node or the file later); only an unreachable server is reported as such.
type Warning struct {
	// Kind is "node" (a node class the server does not have), "model" (a model file the server does not have) or
	// "choice" (a value the server's node does not offer).
	Kind string `json:"kind"`
	Node string `json:"node,omitempty"`
	// Class is the node's class_type; Input the input that names the file or the choice; Value its value.
	Class   string `json:"class_type,omitempty"`
	Input   string `json:"input,omitempty"`
	Value   string `json:"value,omitempty"`
	Message string `json:"message"`
}

// CheckResult is what Check found on one endpoint.
type CheckResult struct {
	Endpoint string `json:"endpoint"`
	// Reachable is false when the server could not be asked; Reason then says why and Warnings is empty.
	Reachable bool      `json:"reachable"`
	Reason    string    `json:"reason,omitempty"`
	Warnings  []Warning `json:"warnings"`
}

// Check compares a workflow with the ComfyUI it runs on: the node classes (GET /object_info) and the model files the
// loader nodes name (GET /models/<folder>, and the options of any other choice input). It does not start the
// bundled ComfyUI: a stopped engine is "not reachable" with the way to start it. Warnings do not block anything.
func (s *Service) Check(ctx context.Context, id string) (CheckResult, error) {
	w, err := s.Repo.Get(ctx, id)
	if err != nil {
		return CheckResult{}, mapNotFound(err)
	}
	return s.CheckGraph(ctx, w.Endpoint, w.APIJSON), nil
}

// CheckGraph is Check for a graph that is not saved (yet) on the endpoint.
func (s *Service) CheckGraph(ctx context.Context, endpoint string, graph []byte) CheckResult {
	res := CheckResult{Endpoint: endpoint, Warnings: []Warning{}}
	g, err := localcfg.ParseGraph(graph)
	if err != nil {
		res.Reason = err.Error()
		return res
	}
	c, err := s.client(ctx, endpoint, false)
	if err != nil {
		res.Reason = reasonOf(err)
		return res
	}
	var info objectInfo
	if err := getJSON(ctx, c, "/object_info", &info); err != nil {
		res.Reason = "ComfyUI に問い合わせられません（" + reasonOf(err) + "）。起動しているか、接続先を確かめてください"
		return res
	}
	res.Reachable = true
	folders := map[string][]string{}
	for _, nodeID := range sortedIDs(g) {
		n := g[nodeID]
		if _, ok := info[n.ClassType]; !ok {
			res.Warnings = append(res.Warnings, Warning{
				Kind: "node", Node: nodeID, Class: n.ClassType,
				Message: fmt.Sprintf("ノード %s（%s）がこの ComfyUI にありません。カスタムノードが必要かもしれません", nodeID, n.ClassType),
			})
			continue
		}
		for _, input := range sortedInputs(n) {
			if w := checkInput(ctx, c, info, folders, nodeID, n, input); w != nil {
				res.Warnings = append(res.Warnings, *w)
			}
		}
	}
	return res
}

// checkInput checks one literal string input: a loader's file against the folder, any other choice against the
// options the node declares.
func checkInput(ctx context.Context, c *localhttp.Client, info objectInfo, folders map[string][]string, nodeID string, n localcfg.Node, input string) *Warning {
	var value string
	if err := json.Unmarshal(n.Inputs[input], &value); err != nil || value == "" {
		return nil
	}
	if folder, ok := folderOf[[2]string{n.ClassType, input}]; ok {
		files, seen := folders[folder]
		if !seen {
			files, _ = modelFolder(ctx, c, folder)
			folders[folder] = files
		}
		if files != nil && !hasFile(files, value) {
			return &Warning{
				Kind: "model", Node: nodeID, Class: n.ClassType, Input: input, Value: value,
				Message: fmt.Sprintf("ノード %s（%s）のモデル %q が ComfyUI の %s にありません", nodeID, n.ClassType, value, folder),
			}
		}
		return nil
	}
	spec, ok := info.inputSpec(n.ClassType, input)
	if !ok {
		return nil
	}
	options, uploads := choices(spec)
	if options == nil || uploads || hasFile(options, value) {
		return nil
	}
	return &Warning{
		Kind: "choice", Node: nodeID, Class: n.ClassType, Input: input, Value: value,
		Message: fmt.Sprintf("ノード %s（%s）の %s の値 %q は、この ComfyUI の選択肢にありません", nodeID, n.ClassType, input, value),
	}
}

// hasFile compares file names the way ComfyUI lists them (path separators of the OS it runs on).
func hasFile(files []string, name string) bool {
	name = strings.ReplaceAll(name, "\\", "/")
	return slices.ContainsFunc(files, func(f string) bool { return strings.ReplaceAll(f, "\\", "/") == name })
}

func sortedIDs(g localcfg.Graph) []string {
	ids := make([]string, 0, len(g))
	for id := range g {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, numericLess)
	return ids
}

func sortedInputs(n localcfg.Node) []string {
	names := make([]string, 0, len(n.Inputs))
	for name := range n.Inputs {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// reasonOf is the message of an error for a person: the refusal text, or the error's own.
func reasonOf(err error) string {
	var in *InvalidError
	if errors.As(err, &in) {
		return in.Message
	}
	return err.Error()
}

// InputTypes names the type of each input of the workflow's nodes the ComfyUI declares (GET /object_info), keyed
// "<node id>|<input>": STRING, INT, FLOAT, BOOLEAN, COMBO (a choice) or the socket type. The assignment editor shows
// it beside the input so a value goes to an input of the right type. An empty map when the server cannot be asked
// (the editor then lists the inputs without types); the bundled ComfyUI is not started for this.
func (s *Service) InputTypes(ctx context.Context, d Detail) map[string]string {
	types := map[string]string{}
	c, err := s.client(ctx, d.Endpoint, false)
	if err != nil {
		return types
	}
	var info objectInfo
	if err := getJSON(ctx, c, "/object_info", &info); err != nil {
		return types
	}
	for _, n := range d.Nodes {
		for _, in := range n.Inputs {
			if spec, ok := info.inputSpec(n.Class, in.Name); ok {
				types[n.ID+"|"+in.Name] = inputType(spec)
			}
		}
	}
	return types
}

// inputType is the type name of an input's declaration: ["INT", {...}] or [[choices...], {...}].
func inputType(spec json.RawMessage) string {
	var parts []json.RawMessage
	if json.Unmarshal(spec, &parts) != nil || len(parts) == 0 {
		return ""
	}
	var name string
	if json.Unmarshal(parts[0], &name) == nil {
		return name
	}
	if options, uploads := choices(spec); options != nil {
		if uploads {
			return "IMAGE_UPLOAD"
		}
		return "COMBO"
	}
	return ""
}
