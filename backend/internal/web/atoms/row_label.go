package atoms

import (
	"strings"

	"github.com/ousiassllc/mam/backend/internal/textutil"
)

// rowLabelRunes is how much of the row's name an aria-label carries: enough to tell rows apart,
// short enough to be read out once per button.
const rowLabelRunes = 60

// RowLabel is the accessible name of a button that repeats down a list: the visible label first
// (WCAG 2.5.3), then the row it acts on, so that "削除" does not read the same on every row.
func RowLabel(label, subject string) string {
	subject = strings.Join(strings.Fields(subject), " ")
	if subject == "" {
		return label
	}
	return label + ": " + textutil.TruncateRunesEllipsis(subject, rowLabelRunes)
}
