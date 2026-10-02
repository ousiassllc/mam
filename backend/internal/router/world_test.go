package router_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

func (a *app) execSQL(q string, args ...any) {
	a.t.Helper()
	require.NoError(a.t, a.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, q, args...)
		return err
	}))
}

func TestWorldState_PageShowsCurrentValuesAndUnknownProjectIs404(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液の縦型15秒CMを作りたい")
	a.execSQL(`INSERT INTO character (id, project_id, name, wardrobe, emotion) VALUES ('c1', ?, 'Mio', 'black_coat_v2', 'anxious')`, id)
	a.execSQL(`INSERT INTO product (id, project_id, name, condition) VALUES ('p1', ?, 'Serum', 'unopened')`, id)
	a.execSQL(`INSERT INTO location (id, project_id, name, time) VALUES ('l1', ?, 'Apartment_A', 'sunset')`, id)

	rec := a.get("/projects/"+id+"/world-state", false)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "<html")
	assert.Contains(t, body, `id="world-state-view"`)
	assert.Contains(t, body, `hx-put="/projects/`+id+`/characters/c1"`)
	assert.Contains(t, body, `hx-put="/projects/`+id+`/products/p1"`)
	assert.Contains(t, body, `hx-put="/projects/`+id+`/locations/l1"`)
	for _, want := range []string{"Mio", "black_coat_v2", "anxious", "Serum", "unopened", "Apartment_A", "sunset", "Camera", "Story"} {
		assert.Contains(t, body, want)
	}

	frag := a.get("/projects/"+id+"/world-state", true)
	assert.NotContains(t, frag.Body.String(), "<html", "HTMX gets the fragment only")
	assert.Contains(t, frag.Body.String(), `id="world-state-view"`)

	assert.Equal(t, http.StatusNotFound, a.get("/projects/nope/world-state", false).Code)
	assert.Equal(t, http.StatusNotFound, a.get("/projects/"+id+"/world-state?branch_id=b1", false).Code, "a Branch that does not exist")
	assert.Contains(t, a.get("/projects/"+id, false).Body.String(), `/projects/`+id+`/world-state"`, "the workspace links to it")
}

func TestWorldState_PutUpdatesAttributesAndAppendsAVersion(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液の縦型15秒CMを作りたい")
	a.execSQL(`INSERT INTO character (id, project_id, name) VALUES ('c1', ?, 'Mio')`, id)
	a.execSQL(`INSERT INTO product (id, project_id, name) VALUES ('p1', ?, 'Serum')`, id)
	a.execSQL(`INSERT INTO location (id, project_id, name) VALUES ('l1', ?, 'Street')`, id)

	rec := a.form(http.MethodPut, "/projects/"+id+"/characters/c1", url.Values{"wardrobe": {" red_dress "}, "hairstyle": {"long_black"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `value="red_dress"`, "the updated view is returned, trimmed")
	assert.Contains(t, rec.Body.String(), `value="long_black"`)
	assert.NotContains(t, rec.Body.String(), "<html")

	require.Equal(t, http.StatusOK, a.form(http.MethodPut, "/projects/"+id+"/products/p1", url.Values{"material": {"frosted_glass"}}).Code)
	require.Equal(t, http.StatusOK, a.form(http.MethodPut, "/projects/"+id+"/locations/l1", url.Values{"weather": {"clear"}}).Code)

	page := a.get("/projects/"+id+"/world-state", false).Body.String()
	for _, want := range []string{"red_dress", "long_black", "frosted_glass", "clear"} {
		assert.Contains(t, page, want)
	}
	var versions int
	require.NoError(t, a.db.Reader().QueryRow(`SELECT COUNT(*) FROM world_state_version WHERE project_id = ?`, id).Scan(&versions))
	assert.Equal(t, 3, versions, "every edit appends a version")
}

func TestWorldState_PutOnALockedFieldIs423AndWritesNothing(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液の縦型15秒CMを作りたい")
	a.execSQL(`INSERT INTO character (id, project_id, name, wardrobe) VALUES ('c1', ?, 'Mio', 'black_coat_v2')`, id)
	a.execSQL(`INSERT INTO product (id, project_id, name) VALUES ('p1', ?, 'Serum')`, id)
	a.execSQL(`INSERT INTO lock (id, project_id, target_type, target_id, parameter_name, mode, value) VALUES ('k1', ?, 'character', 'c1', 'wardrobe', 'lock', 'null')`, id)
	a.execSQL(`INSERT INTO lock (id, project_id, target_type, target_id, parameter_name, mode, value) VALUES ('k2', ?, 'product', 'p1', 'orientation', 'lock', 'null')`, id)

	rec := a.form(http.MethodPut, "/projects/"+id+"/characters/c1", url.Values{"wardrobe": {"red_dress"}, "hairstyle": {"short"}})
	assert.Equal(t, http.StatusLocked, rec.Code)
	assert.Equal(t, "none", rec.Header().Get("HX-Reswap"), "shown as a toast; the view is left as it is")
	assert.Equal(t, http.StatusLocked, a.form(http.MethodPut, "/projects/"+id+"/products/p1", url.Values{"orientation": {"front"}}).Code)

	page := a.get("/projects/"+id+"/world-state", false).Body.String()
	assert.Contains(t, page, "black_coat_v2")
	assert.NotContains(t, page, "red_dress")
	assert.NotContains(t, page, `value="short"`, "an update touching a LOCKed field is refused as a whole")
	assert.Contains(t, page, "LOCK", "the locked fields are marked")

	// A different, unlocked field of the same entity is still editable.
	assert.Equal(t, http.StatusOK, a.form(http.MethodPut, "/projects/"+id+"/characters/c1", url.Values{"hairstyle": {"short"}}).Code)
}

func TestWorldState_PutRejectsBadInput(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液の縦型15秒CMを作りたい")
	a.execSQL(`INSERT INTO character (id, project_id, name) VALUES ('c1', ?, 'Mio')`, id)
	other := a.createProject("別のプロジェクトを作りたい")

	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPut, "/projects/"+id+"/characters/nope", url.Values{"wardrobe": {"x"}}).Code)
	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPut, "/projects/"+other+"/characters/c1", url.Values{"wardrobe": {"x"}}).Code, "another Project's character")
	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPut, "/projects/nope/characters/c1", url.Values{"wardrobe": {"x"}}).Code)
	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPut, "/projects/"+id+"/characters/c1?branch_id=b1", url.Values{"wardrobe": {"x"}}).Code, "a Branch that does not exist")
	assert.Equal(t, http.StatusUnprocessableEntity, a.form(http.MethodPut, "/projects/"+id+"/characters/c1", url.Values{}).Code, "nothing to update")
	assert.Equal(t, http.StatusUnprocessableEntity, a.form(http.MethodPut, "/projects/"+id+"/characters/c1", url.Values{"name": {"x"}}).Code, "name is not an attribute of the form: nothing to update")
	long := make([]byte, domain.MaxWorldValueRunes+1)
	for i := range long {
		long[i] = 'a'
	}
	assert.Equal(t, http.StatusUnprocessableEntity, a.form(http.MethodPut, "/projects/"+id+"/characters/c1", url.Values{"wardrobe": {string(long)}}).Code)

	// An unknown field next to a valid one is a 422 for the whole update (never a silent 200), for
	// every entity kind (#365, #380).
	a.execSQL(`INSERT INTO product (id, project_id, name) VALUES ('p1', ?, 'Serum')`, id)
	a.execSQL(`INSERT INTO location (id, project_id, name) VALUES ('l1', ?, 'Street')`, id)
	for _, path := range []string{"/characters/c1", "/products/p1", "/locations/l1"} {
		field := map[string]string{"/characters/c1": "wardrobe", "/products/p1": "condition", "/locations/l1": "weather"}[path]
		assert.Equal(t, http.StatusUnprocessableEntity, a.form(http.MethodPut, "/projects/"+id+path, url.Values{field: {"kept?"}, "wardorbe": {"typo"}}).Code, path)
		assert.NotContains(t, a.get("/projects/"+id+"/world-state", false).Body.String(), "kept?", "nothing is written")
	}
}
