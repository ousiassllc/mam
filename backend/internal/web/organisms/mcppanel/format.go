// Package mcppanel is the "MCP サーバー" section of the global settings screen: the listening address, the bind
// address form and the bearer tokens (spec/architecture/mcp.md §9).
package mcppanel

import (
	"strconv"
	"time"
)

func formatTime(t time.Time) string { return t.Local().Format("2006-01-02 15:04") }

func maxLength(n int) string {
	if n <= 0 {
		return "64"
	}
	return strconv.Itoa(n)
}
