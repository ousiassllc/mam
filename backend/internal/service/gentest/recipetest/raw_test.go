package recipetest_test

import (
	"encoding/json"
	"testing"

	"github.com/ousiassllc/mam/backend/internal/repository/reciperepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/recipe"
)

// saveRaw writes a recipe row as it is (a recipe whose model was installed when it was saved and is not now).
func saveRaw(t *testing.T, r *gentesttest.Rig, x recipe.Recipe) error {
	t.Helper()
	params, err := json.Marshal(x.Params)
	if err != nil {
		return err
	}
	return reciperepo.New(r.DB).Create(ctx(), reciperepo.Recipe{ID: x.ID, Name: x.Name, Easy: x.Easy, Params: params, CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt})
}
