package recipe

import (
	"encoding/json"

	"github.com/ousiassllc/mam/backend/internal/repository/reciperepo"
)

func fromRow(row reciperepo.Recipe) (Recipe, error) {
	r := Recipe{ID: row.ID, Name: row.Name, Easy: row.Easy, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
	return r, json.Unmarshal(row.Params, &r.Params)
}

func toRow(r Recipe) reciperepo.Recipe {
	params, _ := json.Marshal(r.Params) // a struct of plain values
	return reciperepo.Recipe{ID: r.ID, Name: r.Name, Easy: r.Easy, Params: params, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}
