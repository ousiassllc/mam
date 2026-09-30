package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// ErrInvalidBundle means the bundle is not something this version can import: an unknown table or
// column, a row that is not the Project's, or data the schema rejects. Nothing is written.
var ErrInvalidBundle = errors.New("repository: invalid project bundle")

// bundleTable is a table of the Project Data Model and the condition that selects the rows of one
// Project (:pid is the Project ID).
type bundleTable struct {
	name, where string
}

const (
	byProject = `project_id = :pid`
	byShot    = `shot_id IN (SELECT sh.id FROM shot sh JOIN scene sc ON sc.id = sh.scene_id WHERE sc.project_id = :pid)`
	byTake    = `take_id IN (SELECT t.id FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id
		JOIN shot sh ON sh.id = pv.shot_id JOIN scene sc ON sc.id = sh.scene_id WHERE sc.project_id = :pid)`
)

// bundleTables is every table a bundle carries. A table that belongs to a Project MUST be listed
// here, or in unbundledTables with the reason (TestBundle_EveryProjectTableIsClassified enforces it).
var bundleTables = []bundleTable{
	{"project", `id = :pid`},
	{"project_policy", byProject},
	{"brief", byProject},
	{"asset", byProject},
	{"chat_message", byProject},
	{"character", byProject},
	{"product", byProject},
	{"location", byProject},
	{"script_version", byProject},
	{"scene", byProject},
	{"shot", `scene_id IN (SELECT id FROM scene WHERE project_id = :pid)`},
	{"prompt_version", byShot},
	{"take", `prompt_version_id IN (SELECT pv.id FROM prompt_version pv JOIN shot sh ON sh.id = pv.shot_id
		JOIN scene sc ON sc.id = sh.scene_id WHERE sc.project_id = :pid)`},
	{"judge_result", byTake},
	{"repair_action", `judge_result_id IN (SELECT jr.id FROM judge_result jr WHERE jr.` + byTake + `)`},
	{"repair_shadow", `repair_action_id IN (SELECT ra.id FROM repair_action ra WHERE ra.judge_result_id IN
		(SELECT jr.id FROM judge_result jr WHERE jr.` + byTake + `))`},
	{"world_state_version", byProject},
	{"state_diff", byShot},
	{"edit_timeline_version", byProject},
	{"editor_judge_result", `edit_timeline_version_id IN (SELECT id FROM edit_timeline_version WHERE project_id = :pid)`},
	{"audio_timeline_version", byProject},
	{"export", byProject},
	{"lock", byProject},
	{"cost_ledger_entry", byProject},
	{"reference_item", byProject},
	{"branch", byProject},
	{"branch_version", `branch_id IN (SELECT id FROM branch WHERE project_id = :pid)`},
}

// unbundledTables are the tables a bundle leaves out, with the reason.
var unbundledTables = map[string]string{
	"job":              "background job queue: runtime state of this instance (a progress card whose job is gone is shown as finished)",
	"goose_db_version": "migration bookkeeping of this instance",
	"sqlite_sequence":  "SQLite internal",
}

// BundleRepository reads the Project Data Model of a Project out of the database and writes it into
// a database (spec/requirements/non-functional.md §8). All writes go through DB.Write.
type BundleRepository struct {
	db *DB
}

// NewBundleRepository returns a BundleRepository backed by db.
func NewBundleRepository(db *DB) *BundleRepository { return &BundleRepository{db: db} }

// Export returns every row of the Project in one consistent snapshot, or ErrNotFound.
func (r *BundleRepository) Export(ctx context.Context, projectID string) (domain.BundleData, error) {
	tx, err := r.db.Reader().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.BundleData{}, fmt.Errorf("begin export: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := exists(ctx, tx, `SELECT 1 FROM project WHERE id = ?`, projectID); err != nil {
		return domain.BundleData{}, err
	}
	data := domain.BundleData{ProjectID: projectID}
	for _, t := range bundleTables {
		rows, err := readBundleRows(ctx, tx, t, projectID)
		if err != nil {
			return domain.BundleData{}, fmt.Errorf("export %s: %w", t.name, err)
		}
		data.Tables = append(data.Tables, domain.BundleTable{Name: t.name, Rows: rows})
	}
	return data, nil
}

func readBundleRows(ctx context.Context, tx *sql.Tx, t bundleTable, projectID string) ([]domain.BundleRow, error) {
	rows, err := tx.QueryContext(ctx, `SELECT * FROM `+t.name+` WHERE `+t.where+` ORDER BY rowid`, sql.Named("pid", projectID)) //nolint:gosec // name and where are constants of bundleTables
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := []domain.BundleRow{}
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(domain.BundleRow, len(cols))
		for i, c := range cols {
			if b, ok := values[i].([]byte); ok {
				values[i] = string(b) // the schema has no BLOB column; text stored as bytes is text
			}
			row[c] = values[i]
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// Import writes the Project into the database in one transaction. It returns ErrConflict when the
// database already has a Project with that ID and ErrInvalidBundle (nothing written) when the data
// is not importable: an unknown table or column, a row of another Project, or a row the schema or
// a foreign key rejects.
func (r *BundleRepository) Import(ctx context.Context, data domain.BundleData) error {
	if err := checkBundle(data); err != nil {
		return err
	}
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := exists(ctx, tx, `SELECT 1 FROM project WHERE id = ?`, data.ProjectID); err == nil {
			return ErrConflict
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		// Rows refer to each other in cycles (shot.selected_take_id, take, ...): check the foreign
		// keys once, at the end, instead of per row.
		if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
			return err
		}
		for _, t := range data.Tables {
			if err := insertBundleTable(ctx, tx, t); err != nil {
				return fmt.Errorf("%w: %s: %w", ErrInvalidBundle, t.Name, err)
			}
		}
		// An imported Project has no running job: what was in flight when it was exported is not.
		if _, err := tx.ExecContext(ctx,
			`UPDATE shot SET state = ? WHERE state IN (?, ?, ?) AND scene_id IN (SELECT id FROM scene WHERE project_id = ?)`,
			domain.ShotPlanning, domain.ShotGenerating, domain.ShotJudging, domain.ShotRepairing, data.ProjectID); err != nil {
			return err
		}
		violations, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
		if err != nil {
			return err
		}
		defer func() { _ = violations.Close() }()
		if violations.Next() {
			var table, parent string
			var rowid, fk sql.NullInt64
			if err := violations.Scan(&table, &rowid, &parent, &fk); err != nil {
				return err
			}
			return fmt.Errorf("%w: %s refers to a missing %s", ErrInvalidBundle, table, parent)
		}
		return violations.Err()
	})
	if errors.Is(err, ErrConflict) || errors.Is(err, ErrInvalidBundle) {
		return err
	}
	if err != nil {
		return fmt.Errorf("import bundle: %w", err)
	}
	return nil
}

// checkBundle verifies what can be verified without the database: known tables, one Project row
// with the bundle's ID, and that every row that names a Project names this one.
func checkBundle(data domain.BundleData) error {
	if !domain.ValidBundleProjectID(data.ProjectID) {
		return fmt.Errorf("%w: project id", ErrInvalidBundle)
	}
	known := map[string]bool{}
	for _, t := range bundleTables {
		known[t.name] = true
	}
	seen := map[string]bool{}
	for _, t := range data.Tables {
		if !known[t.Name] || seen[t.Name] {
			return fmt.Errorf("%w: table %q", ErrInvalidBundle, t.Name)
		}
		seen[t.Name] = true
		for _, row := range t.Rows {
			if t.Name == "project" && row["id"] != data.ProjectID {
				return fmt.Errorf("%w: a project row of another project", ErrInvalidBundle)
			}
			if pid, ok := row["project_id"]; ok && pid != data.ProjectID {
				return fmt.Errorf("%w: %s has a row of another project", ErrInvalidBundle, t.Name)
			}
		}
	}
	for _, t := range data.Tables {
		if t.Name == "project" && len(t.Rows) == 1 {
			return nil
		}
	}
	return fmt.Errorf("%w: exactly one project row is required", ErrInvalidBundle)
}

func insertBundleTable(ctx context.Context, tx *sql.Tx, t domain.BundleTable) error {
	if len(t.Rows) == 0 {
		return nil
	}
	known, err := tableColumns(ctx, tx, t.Name)
	if err != nil {
		return err
	}
	for _, row := range t.Rows {
		cols := make([]string, 0, len(row))
		for c := range row {
			if !slices.Contains(known, c) {
				return fmt.Errorf("unknown column %q", c)
			}
			cols = append(cols, c)
		}
		slices.Sort(cols)
		args := make([]any, len(cols))
		for i, c := range cols {
			args[i] = bundleArg(row[c])
		}
		q := `INSERT INTO ` + t.Name + ` (` + strings.Join(cols, ", ") + `) VALUES (` + strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ") + `)` //nolint:gosec // the table is one of bundleTables and every column was checked against the schema
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return err
		}
	}
	return nil
}

// bundleArg turns a value decoded from data.json (numbers arrive as json.Number) into a SQL argument.
func bundleArg(v any) any {
	n, ok := v.(json.Number)
	if !ok {
		return v
	}
	if i, err := n.Int64(); err == nil {
		return i
	}
	if f, err := n.Float64(); err == nil {
		return f
	}
	return n.String()
}

func tableColumns(ctx context.Context, tx *sql.Tx, table string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}
