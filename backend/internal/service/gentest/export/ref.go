package export

import (
	"errors"
	"strconv"
	"strings"
)

// Ref names the results of one ZIP of a Plan, as a string that fits the resource id of a download ticket (the MCP file endpoint
// keeps it in memory next to the random ticket value). A download built from a Ref serves the results that were chosen when the
// URL was issued, not whatever the selection would match later.
type Ref struct {
	// Part is the place of this ZIP among Parts (from 1); First is the number of its first item and Total the number of all
	// items of the export, so that the files of every part keep the numbers of the whole.
	Part, Parts, First, Total int
	IDs                       []string
}

// String is "<part>/<parts>/<first>/<total>/<id>,<id>,...". Result ids never contain "/" or "," (cleanIDs).
func (r Ref) String() string {
	return strings.Join([]string{strconv.Itoa(r.Part), strconv.Itoa(r.Parts), strconv.Itoa(r.First), strconv.Itoa(r.Total), strings.Join(r.IDs, ",")}, "/")
}

var errBadRef = errors.New("export: malformed ref")

// ParseRef is the inverse of Ref.String.
func ParseRef(s string) (Ref, error) {
	f := strings.Split(s, "/")
	if len(f) != 5 {
		return Ref{}, errBadRef
	}
	var n [4]int
	for i := range n {
		v, err := strconv.Atoi(f[i])
		if err != nil || v < 1 {
			return Ref{}, errBadRef
		}
		n[i] = v
	}
	ids, err := cleanIDs(strings.Split(f[4], ","))
	if err != nil || len(ids) == 0 || n[0] > n[1] || n[1] > MaxParts {
		return Ref{}, errBadRef
	}
	return Ref{Part: n[0], Parts: n[1], First: n[2], Total: n[3], IDs: ids}, nil
}
