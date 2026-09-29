package promptplanner

import (
	"encoding/json"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

func jsonText(s domain.ShotSpec) string {
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil { // plain strings and numbers
		panic(err)
	}
	return string(raw)
}
