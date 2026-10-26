package workflows

import "strings"

func joinList(items []string) string { return strings.Join(items, "、") }

func currentMark(current bool) string {
	if current {
		return "（現在）"
	}
	return ""
}
