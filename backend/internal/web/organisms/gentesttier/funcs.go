package gentesttier

import "strconv"

func itoa(n int) string { return strconv.Itoa(n) }

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
