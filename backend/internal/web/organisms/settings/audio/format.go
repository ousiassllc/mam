package audio

import (
	"strconv"
	"strings"
)

func itoa(n int) string { return strconv.Itoa(n) }

// join writes names as a list of the screen: 「a」、「b」.
func join(names []string) string { return "「" + strings.Join(names, "」、「") + "」" }

// cloudChecked is whether the cloud radio is on: the saved provider is the cloud, or it is the local one that this PC cannot run (the
// radio that cannot be chosen is not shown as the choice, and a save then switches to the cloud).
func cloudChecked(p Provider) bool {
	return p.Selected != LocalValue || p.Local == nil || !p.Local.Supported
}
