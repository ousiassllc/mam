package commercial

import (
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
)

// filterOptions are the choices of the filter, in the order they are shown.
func filterOptions() []modellicense.Filter { return modellicense.Filters() }

// noteClass is the box of a Note: red for 商用NG, amber with an edge for the others (the colours of the meanings, color-rules.md).
func noteClass(v View) string {
	base := "flex flex-col gap-1 rounded-md border p-2 text-xs break-words "
	if v.Level == modellicense.No {
		return base + "border-red-500/60 bg-red-500/10 text-red-300"
	}
	return base + "border-amber-500/60 bg-amber-500/10 text-amber-300"
}

func noteIcon(v View) atoms.IconName {
	switch v.Level {
	case modellicense.Conditional:
		return atoms.IconBadgeAlert
	case modellicense.No:
		return atoms.IconBan
	}
	return atoms.IconBadgeHelp
}
