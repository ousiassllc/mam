package audio

import (
	"strconv"
	"strings"
)

func itoa(n int) string { return strconv.Itoa(n) }

// join writes names as a list of the screen: 「a」、「b」.
func join(names []string) string { return "「" + strings.Join(names, "」、「") + "」" }

// cloudChecked is whether the cloud radio is on: the saved provider is the cloud, or it is a local one that this PC cannot run (the
// radio that cannot be chosen is not shown as the choice, and a save then switches to the cloud).
func cloudChecked(p Provider) bool {
	return !p.ChosenLocal() && !p.As(WhichIrodori).ChosenLocal()
}

// NeedsCommercialNote is whether the card says "商用注意": the provider has a condition on its commercial use, or weights or
// dependencies whose commercial use is not clear.
func (l *Local) NeedsCommercialNote() bool { return l.Condition != "" || len(l.Unclear) > 0 }

// localSuffix tells the second local provider's test ids from the first's ("" for the first).
func localSuffix(p Provider) string {
	if p.slug() == WhichLocal {
		return ""
	}
	return "-" + p.slug()
}
