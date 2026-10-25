package appserver

import (
	"fmt"
	"strings"
)

// exitText is the sentence about a process that ended: the exit code and the end of its standard error. The tail is
// the process's own log text, which holds no token (the app-server does not print one).
func exitText(code int, tail string) string {
	msg := fmt.Sprintf("Codex app-server が終了しました（終了コード %d）", code)
	if t := strings.TrimSpace(tail); t != "" {
		msg += "\n--- 標準エラーの末尾 ---\n" + t
	}
	return msg
}
