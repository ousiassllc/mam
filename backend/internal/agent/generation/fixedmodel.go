package generation

import (
	"slices"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// FixedModel is the Video Model the user fixed for the Shot, or "" (AUTO, or no known model chosen).
func FixedModel(l domain.ParameterLocks) domain.ModelName {
	m := domain.ModelName(l.Text(domain.ParamVideoModel))
	if l.Mode(domain.ParamVideoModel) == domain.LockAuto || !slices.Contains(domain.VideoModels(), m) {
		return ""
	}
	return m
}
