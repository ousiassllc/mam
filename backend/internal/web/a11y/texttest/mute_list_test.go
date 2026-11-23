package texttest_test

import (
	"fmt"
)

// mutedLists lists the <ul>/<ol> that say text-mute themselves: a list is a run of items, each of which says its own colour, because what the items say
// (a constraint's text, a version) is only a caption when its own element says so. A list that is --mute makes every text in it --mute without anyone looking
// at it (C6).
func mutedLists(root *muteNode) []string {
	var found []string
	var walk func(n *muteNode)
	walk = func(n *muteNode) {
		if (n.tag == "ul" || n.tag == "ol") && n.classes["text-mute"] {
			found = append(found, fmt.Sprintf("line %d <%s>: the list says text-mute", n.line, n.tag))
		}
		for _, k := range n.kids {
			walk(k)
		}
	}
	walk(root)
	return found
}
