// Package graphui turns a ComfyUI workflow in API format (what mam runs) into the editor's own format (what the node screen opens), with the help of what the
// server says of its nodes (GET /object_info). Issue #977 uses it for "今の設定を ComfyUI で開く": the graph the form's values make is put into the
// ComfyUI server's workflows and opened in the node screen. The editor's format keeps what the API format leaves out: where each node sits, the order of
// the widgets of a node and the links as a table. A node that sits nowhere is placed in columns by how far it is from the nodes that have no input.
package graphui

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
)

// Node sizes and gaps of the layout.
const (
	nodeWidth  = 320
	rowHeight  = 28
	headHeight = 56
	columnGap  = 60
	rowGap     = 40
)

// Convert returns the editor's format of the API-format graph api. info is the answer of GET /object_info of the server that runs it: a node whose class it
// does not know is an error (the editor cannot draw it, and the widgets of a node cannot be put in order without its class).
func Convert(api, info json.RawMessage) (json.RawMessage, error) {
	var g map[string]apiNode
	if err := json.Unmarshal(api, &g); err != nil || len(g) == 0 {
		return nil, errors.New("the graph is not in API format")
	}
	var classes map[string]class
	if err := json.Unmarshal(info, &classes); err != nil {
		return nil, fmt.Errorf("the node information cannot be read: %w", err)
	}
	b := builder{g: g, classes: classes, ids: map[string]int{}, depth: map[string]int{}}
	if err := b.number(); err != nil {
		return nil, err
	}
	nodes := make([]uiNode, 0, len(g))
	for _, id := range b.order {
		n, err := b.node(id)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	b.layout(nodes)
	for i := range nodes {
		nodes[i].Order = i
	}
	out := uiGraph{LastNode: b.ids[b.order[len(b.order)-1]], LastLink: len(b.links), Nodes: nodes, Links: b.links, Groups: []any{}, Config: map[string]any{}, Extra: map[string]any{}, Version: 0.4}
	return json.Marshal(out)
}

type builder struct {
	g       map[string]apiNode
	classes map[string]class
	ids     map[string]int // the id of the API format -> the number the editor uses
	order   []string       // the ids of the API format, by number
	depth   map[string]int
	links   [][]any
	// outLinks are the link ids that leave each output: node id -> slot -> ids.
	outLinks map[string]map[int][]int
}

// number gives each node a whole number: its own id when it is one (the editor's ids are numbers), the next free number otherwise; and orders the nodes by it.
func (b *builder) number() error {
	var names []string
	for id := range b.g {
		names = append(names, id)
	}
	slices.SortFunc(names, func(x, y string) int {
		if len(x) != len(y) {
			return len(x) - len(y)
		}
		if x < y {
			return -1
		}
		return 1
	})
	used := map[int]bool{}
	var rest []string
	for _, id := range names {
		if n, err := strconv.Atoi(id); err == nil && n > 0 && !used[n] {
			b.ids[id], used[n] = n, true
		} else {
			rest = append(rest, id)
		}
	}
	next := 1
	for _, id := range rest {
		for used[next] {
			next++
		}
		b.ids[id], used[next] = next, true
	}
	b.order = names
	slices.SortFunc(b.order, func(x, y string) int { return b.ids[x] - b.ids[y] })
	b.outLinks = map[string]map[int][]int{}
	return nil
}

func (b *builder) node(id string) (uiNode, error) {
	n := b.g[id]
	c, ok := b.classes[n.ClassType]
	if !ok {
		return uiNode{}, fmt.Errorf("node %s (%s) is not a node of the ComfyUI server", id, n.ClassType)
	}
	out := uiNode{
		ID: b.ids[id], Type: n.ClassType, Title: n.Meta.Title, Flags: map[string]any{}, Properties: map[string]any{"Node name for S&R": n.ClassType},
		Inputs: []uiInput{}, Outputs: []uiOutput{}, Widgets: []any{},
	}
	for _, s := range c.inputs() {
		kind, opts, isWidget := widget(s.spec)
		in := uiInput{Name: s.name, Type: kind}
		if kind == "" {
			in.Type = "*"
		}
		if isWidget {
			in.Widget = map[string]string{"name": s.name}
		}
		raw, given := n.Inputs[s.name]
		if target, slotNo, isLink := link(raw); given && isLink {
			linkID, err := b.link(target, slotNo, id, len(out.Inputs))
			if err != nil {
				return uiNode{}, err
			}
			in.Link = &linkID
		}
		out.Inputs = append(out.Inputs, in)
		if isWidget {
			out.Widgets = append(out.Widgets, b.widgetValues(kind, opts, raw, given && in.Link == nil)...)
		}
	}
	for i, t := range c.Output {
		name := t
		if i < len(c.OutputName) && c.OutputName[i] != "" {
			name = c.OutputName[i]
		}
		out.Outputs = append(out.Outputs, uiOutput{Name: name, Type: t, Slot: i})
	}
	out.Size = [2]int{nodeWidth, headHeight + rowHeight*max(len(out.Inputs), len(out.Outputs), 1)}
	return out, nil
}

// widgetValues are the entries of widgets_values a field adds: its value, and the extra fields the editor puts after some (the seed's "control after
// generate", which is left on "fixed" so that the seed of the graph stays; the upload button of an image).
func (b *builder) widgetValues(kind string, opts map[string]json.RawMessage, raw json.RawMessage, literal bool) []any {
	var v any
	if !literal || json.Unmarshal(raw, &v) != nil {
		v = defaultOf(kind, opts)
	}
	out := []any{v}
	if string(opts["control_after_generate"]) == "true" {
		out = append(out, "fixed")
	}
	if string(opts["image_upload"]) == "true" {
		out = append(out, "image")
	}
	return out
}

// link is the node and the output a link value ["<id>", <slot>] points to.
func link(raw json.RawMessage) (target string, slot int, ok bool) {
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) != nil || len(arr) != 2 || json.Unmarshal(arr[0], &target) != nil || json.Unmarshal(arr[1], &slot) != nil {
		return "", 0, false
	}
	return target, slot, true
}

// link records the link from the output slot of target to input number inputNo of node to, and returns its id; its type is the output's.
func (b *builder) link(target string, slotNo int, to string, inputNo int) (int, error) {
	src, ok := b.g[target]
	if !ok {
		return 0, fmt.Errorf("node %s links to the missing node %s", to, target)
	}
	c, ok := b.classes[src.ClassType]
	if !ok || slotNo < 0 || slotNo >= len(c.Output) {
		return 0, fmt.Errorf("node %s links to output %d of node %s (%s), which has none", to, slotNo, target, src.ClassType)
	}
	id := len(b.links) + 1
	typ := c.Output[slotNo]
	b.links = append(b.links, []any{id, b.ids[target], slotNo, b.ids[to], inputNo, typ})
	if b.outLinks[target] == nil {
		b.outLinks[target] = map[int][]int{}
	}
	b.outLinks[target][slotNo] = append(b.outLinks[target][slotNo], id)
	return id, nil
}

// layout puts the nodes in columns: a node is one column to the right of the furthest node that feeds it. It also gives each output the links that leave it.
func (b *builder) layout(nodes []uiNode) {
	var depthOf func(id string, seen map[string]bool) int
	depthOf = func(id string, seen map[string]bool) int {
		if d, ok := b.depth[id]; ok {
			return d
		}
		if seen[id] {
			return 0 // a cycle: not a graph the server runs; the column is a guess
		}
		seen[id] = true
		d := 0
		for _, raw := range b.g[id].Inputs {
			if target, _, ok := link(raw); ok {
				d = max(d, depthOf(target, seen)+1)
			}
		}
		b.depth[id] = d
		return d
	}
	y := map[int]int{}
	for i, id := range b.order {
		col := depthOf(id, map[string]bool{})
		nodes[i].Pos = [2]int{columnGap + col*(nodeWidth+columnGap), columnGap + y[col]}
		y[col] += nodes[i].Size[1] + rowGap
		for slotNo, ids := range b.outLinks[id] {
			if slotNo < len(nodes[i].Outputs) {
				nodes[i].Outputs[slotNo].Links = ids
			}
		}
	}
}
