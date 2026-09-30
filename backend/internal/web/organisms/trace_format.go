package organisms

import (
	"bytes"
	"encoding/json"
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
)

// prettyJSON indents raw for display; empty raw shows a dash and text that is not JSON is shown as is.
func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "—"
	}
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return string(raw)
	}
	return out.String()
}

func worldVersionText(v int) string {
	if v == 0 {
		return "なし（Shot 完了前、または記録前の Take）"
	}
	return "v" + strconv.Itoa(v)
}

func judgeTone(v domain.JudgeVerdict) atoms.BadgeTone {
	if v == domain.JudgePass {
		return atoms.BadgeOK
	}
	return atoms.BadgeError
}
