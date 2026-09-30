package repository_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// seedProject inserts a valid project and returns its id.
func seedProject(t *testing.T, db *repository.DB, id string) {
	t.Helper()
	require.NoError(t, exec(t, db,
		`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy)
		 VALUES (?, 'auto', 'key_check', 'product_cm', 'balanced')`, id))
}

func TestSchema_M1TablesExistAndM2TablesDoNot(t *testing.T) {
	db := openTestDB(t)
	tables := map[string]bool{}
	rows, err := db.Reader().Query(`SELECT name FROM sqlite_master WHERE type = 'table'`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		tables[name] = true
	}

	for _, name := range []string{
		"project", "brief", "asset", "chat_message", "character", "product", "location",
		"script_version", "scene", "shot", "prompt_version", "take", "judge_result",
		"repair_action", "world_state_version", "state_diff", "edit_timeline_version",
		"audio_timeline_version", "editor_judge_result", "export", "lock",
		"cost_ledger_entry", "job",
	} {
		assert.True(t, tables[name], "missing M1 table %s", name)
	}
	for _, name := range []string{"note", "note_version", "note_promotion", "branch", "branch_version", "brand_constraint"} {
		assert.False(t, tables[name], "M2 table %s must not be in the M1 schema", name)
	}
}

func TestSchema_ProjectEnumsMatchDomain(t *testing.T) {
	db := openTestDB(t)
	for i, s := range domain.ProjectStates() {
		err := exec(t, db,
			`INSERT INTO project (id, state, mode, confirmation_level, production_template, quality_policy)
			 VALUES (?, ?, 'auto', 'key_check', 'product_cm', 'balanced')`, string(rune('a'+i)), string(s))
		assert.NoError(t, err, "state %s", s)
	}
	err := exec(t, db,
		`INSERT INTO project (id, state, mode, confirmation_level, production_template, quality_policy)
		 VALUES ('bad', 'NOPE', 'auto', 'key_check', 'product_cm', 'balanced')`)
	assert.Error(t, err)

	for col, value := range map[string]string{
		"mode": "manual", "confirmation_level": "always", "production_template": "movie", "quality_policy": "slow",
	} {
		vals := map[string]string{
			"mode": "auto", "confirmation_level": "key_check", "production_template": "product_cm", "quality_policy": "balanced",
		}
		vals[col] = value
		err := exec(t, db,
			`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('x', ?, ?, ?, ?)`,
			vals["mode"], vals["confirmation_level"], vals["production_template"], vals["quality_policy"])
		assert.Error(t, err, "invalid %s must be rejected", col)
	}
}

func TestSchema_ShotStatesMatchDomain(t *testing.T) {
	db := openTestDB(t)
	seedProject(t, db, "p")
	require.NoError(t, exec(t, db, `INSERT INTO scene (id, project_id, order_index) VALUES ('sc', 'p', 0)`))
	for i, s := range domain.ShotStates() {
		assert.NoError(t, exec(t, db,
			`INSERT INTO shot (id, scene_id, order_index, state) VALUES (?, 'sc', ?, ?)`, string(rune('a'+i)), i, string(s)), "state %s", s)
	}
	assert.Error(t, exec(t, db, `INSERT INTO shot (id, scene_id, order_index, state) VALUES ('bad', 'sc', 9, 'READY')`))
}

func TestSchema_UniqueConstraints(t *testing.T) {
	db := openTestDB(t)
	seedProject(t, db, "p")

	require.NoError(t, exec(t, db, `INSERT INTO brief (id, project_id) VALUES ('b1', 'p')`))
	assert.Error(t, exec(t, db, `INSERT INTO brief (id, project_id) VALUES ('b2', 'p')`), "brief is 1:1 with project")

	require.NoError(t, exec(t, db, `INSERT INTO scene (id, project_id, order_index) VALUES ('s1', 'p', 0)`))
	assert.Error(t, exec(t, db, `INSERT INTO scene (id, project_id, order_index) VALUES ('s2', 'p', 0)`))

	require.NoError(t, exec(t, db, `INSERT INTO script_version (id, project_id, version_number, content) VALUES ('v1', 'p', 1, 'x')`))
	assert.Error(t, exec(t, db, `INSERT INTO script_version (id, project_id, version_number, content) VALUES ('v2', 'p', 1, 'y')`))

	require.NoError(t, exec(t, db,
		`INSERT INTO lock (id, project_id, target_type, target_id, parameter_name, mode) VALUES ('l1', 'p', 'character', 'c', 'wardrobe', 'lock')`))
	assert.Error(t, exec(t, db,
		`INSERT INTO lock (id, project_id, target_type, target_id, parameter_name, mode) VALUES ('l2', 'p', 'character', 'c', 'wardrobe', 'auto')`))
	assert.Error(t, exec(t, db,
		`INSERT INTO lock (id, project_id, target_type, target_id, parameter_name, mode) VALUES ('l3', 'p', 'character', 'c', 'hairstyle', 'freeze')`))
}

func TestSchema_TakeAndJudgeChecks(t *testing.T) {
	db := openTestDB(t)
	seedProject(t, db, "p")
	require.NoError(t, exec(t, db, `INSERT INTO scene (id, project_id, order_index) VALUES ('sc', 'p', 0)`))
	require.NoError(t, exec(t, db, `INSERT INTO shot (id, scene_id, order_index) VALUES ('sh', 'sc', 0)`))
	require.NoError(t, exec(t, db, `INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot) VALUES ('pv', 'sh', 1, '{}')`))
	assert.Error(t, exec(t, db, `INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot) VALUES ('pv2', 'sh', 2, 'not json')`))

	assert.Error(t, exec(t, db, `INSERT INTO take (id, prompt_version_id, model_name, status) VALUES ('t0', 'pv', 'kling', 'draft')`))
	for i, status := range []string{"candidate", "ai_pick", "selected", "rejected"} {
		assert.NoError(t, exec(t, db,
			`INSERT INTO take (id, prompt_version_id, model_name, status) VALUES (?, 'pv', 'kling', ?)`, string(rune('a'+i)), status))
	}

	// judge_result.take_id is not UNIQUE: repeated Judge runs are kept as history.
	assert.NoError(t, exec(t, db, `INSERT INTO judge_result (id, take_id, result) VALUES ('j1', 'a', 'fail')`))
	assert.NoError(t, exec(t, db, `INSERT INTO judge_result (id, take_id, result) VALUES ('j2', 'a', 'pass')`))
	assert.Error(t, exec(t, db, `INSERT INTO judge_result (id, take_id, result) VALUES ('j3', 'a', 'maybe')`))
}

func TestSchema_ForeignKeysAreEnforced(t *testing.T) {
	db := openTestDB(t)
	assert.Error(t, exec(t, db, `INSERT INTO scene (id, project_id, order_index) VALUES ('s', 'missing', 0)`))
}

func TestSchema_ChatMessageChecks(t *testing.T) {
	db := openTestDB(t)
	seedProject(t, db, "p")
	insert := func(id, role, kind string, approval any) error {
		return exec(t, db,
			`INSERT INTO chat_message (id, project_id, role, kind, approval_status) VALUES (?, 'p', ?, ?, ?)`, id, role, kind, approval)
	}

	assert.NoError(t, insert("m1", "user", "text", nil))
	assert.Error(t, insert("m2", "user", "question", nil), "role=user allows only text")
	assert.NoError(t, insert("m3", "assistant", "approval", "pending"))
	assert.Error(t, insert("m4", "assistant", "approval", nil), "approval card needs a status")
	assert.Error(t, insert("m5", "assistant", "text", "pending"), "only approval cards carry a status")
	assert.Error(t, insert("m6", "assistant", "approval", "maybe"))
	assert.Error(t, insert("m7", "assistant", "chart", nil))
}

func TestSchema_ChatMessageIsAppendOnlyExceptApprovalStatus(t *testing.T) {
	db := openTestDB(t)
	seedProject(t, db, "p")
	require.NoError(t, exec(t, db,
		`INSERT INTO chat_message (id, project_id, role, kind, content, approval_status) VALUES ('m', 'p', 'assistant', 'approval', 'hi', 'pending')`))

	assert.Error(t, exec(t, db, `UPDATE chat_message SET content = 'edited' WHERE id = 'm'`))
	assert.NoError(t, exec(t, db, `UPDATE chat_message SET approval_status = 'approved' WHERE id = 'm'`))
}

func TestSchema_DeletingProjectCascades(t *testing.T) {
	db := openTestDB(t)
	seedProject(t, db, "p")
	require.NoError(t, exec(t, db, `INSERT INTO scene (id, project_id, order_index) VALUES ('sc', 'p', 0)`))
	require.NoError(t, exec(t, db, `INSERT INTO shot (id, scene_id, order_index) VALUES ('sh', 'sc', 0)`))
	require.NoError(t, exec(t, db, `INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot) VALUES ('pv', 'sh', 1, '{}')`))
	require.NoError(t, exec(t, db, `INSERT INTO take (id, prompt_version_id, model_name) VALUES ('t', 'pv', 'kling')`))
	require.NoError(t, exec(t, db, `UPDATE shot SET selected_take_id = 't' WHERE id = 'sh'`))

	require.NoError(t, exec(t, db, `DELETE FROM project WHERE id = 'p'`))
	for _, table := range []string{"scene", "shot", "prompt_version", "take"} {
		var n int
		require.NoError(t, db.Reader().QueryRow(`SELECT COUNT(*) FROM `+table).Scan(&n))
		assert.Zero(t, n, table)
	}
}

func TestSchema_JobEnumsMatchDomain(t *testing.T) {
	db := openTestDB(t)
	for i, k := range domain.JobKinds() {
		assert.NoError(t, exec(t, db, `INSERT INTO job (id, kind) VALUES (?, ?)`, string(rune('a'+i)), string(k)), "kind %s", k)
	}
	for i, s := range domain.JobStatuses() {
		assert.NoError(t, exec(t, db, `INSERT INTO job (id, kind, status) VALUES (?, 'judge', ?)`, "s"+string(rune('a'+i)), string(s)), "status %s", s)
	}
	assert.Error(t, exec(t, db, `INSERT INTO job (id, kind) VALUES ('x', 'render')`))
}
