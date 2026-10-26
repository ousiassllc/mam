package mcp

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
