// Package bundle reads the Project Data Model of one Project out of the database and writes it into
// a database (the Project bundle: export and import). It is the repository of the Project data
// portability and keeps its own table list, separate from the per-aggregate repositories.
package bundle

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// ErrInvalid means the bundle is not something this version can import: an unknown table or
// column, a row that is not the Project's, or data the schema rejects. Nothing is written.
var ErrInvalid = errors.New("repository: invalid project bundle")

// bundleTable is a table of the Project Data Model and the condition that selects the rows of one
// Project (:pid is the Project ID).
type bundleTable struct {
	name, where string
	// detach are columns that point outside the Project (at its Series, at another Episode's World
	// State). The bundle is self-contained, so they are exported empty: the imported Project stands alone.
	detach []string
	// noID marks a table whose primary key is not an id column (project_policy: its project_id).
	noID bool
}

const (
	byProject = `project_id = :pid`
	byShot    = `shot_id IN (SELECT sh.id FROM shot sh JOIN scene sc ON sc.id = sh.scene_id WHERE sc.project_id = :pid)`
	byTake    = `take_id IN (SELECT t.id FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id
		JOIN shot sh ON sh.id = pv.shot_id JOIN scene sc ON sc.id = sh.scene_id WHERE sc.project_id = :pid)`
)

// bundleTables is every table a bundle carries. A table that belongs to a Project MUST be listed
// here, or in unbundledTables with the reason (TestBundle_EveryTableIsBundledOrHasAReasonNotToBe enforces it).
var bundleTables = []bundleTable{
	{name: "project", where: `id = :pid`, detach: []string{"series_id", "episode_number", "inherited_series_version_id", "inherited_world_state_version_id"}},
	{name: "project_policy", where: byProject, noID: true},
	{name: "brief", where: byProject},
	{name: "asset", where: byProject},
	{name: "chat_message", where: byProject},
	{name: "character", where: byProject, detach: []string{"origin_id"}},
	{name: "product", where: byProject, detach: []string{"origin_id"}},
	{name: "location", where: byProject, detach: []string{"origin_id"}},
	{name: "script_version", where: byProject},
	{name: "scene", where: byProject},
	{name: "shot", where: `scene_id IN (SELECT id FROM scene WHERE project_id = :pid)`},
	{name: "prompt_version", where: byShot},
	{name: "take", where: `prompt_version_id IN (SELECT pv.id FROM prompt_version pv JOIN shot sh ON sh.id = pv.shot_id
		JOIN scene sc ON sc.id = sh.scene_id WHERE sc.project_id = :pid)`},
	{name: "judge_result", where: byTake},
	{name: "repair_action", where: `judge_result_id IN (SELECT jr.id FROM judge_result jr WHERE jr.` + byTake + `)`},
	{name: "repair_shadow", where: `repair_action_id IN (SELECT ra.id FROM repair_action ra WHERE ra.judge_result_id IN
		(SELECT jr.id FROM judge_result jr WHERE jr.` + byTake + `))`},
	{name: "world_state_version", where: byProject},
	{name: "state_diff", where: byShot},
	{name: "edit_timeline_version", where: byProject},
	{name: "editor_judge_result", where: `edit_timeline_version_id IN (SELECT id FROM edit_timeline_version WHERE project_id = :pid)`},
	{name: "reshoot_request", where: byProject},
	{name: "audio_timeline_version", where: byProject},
	{name: "voice_clone", where: byProject},
	{name: "final_qc_result", where: byProject},
	{name: "export", where: byProject},
	{name: "lock", where: byProject},
	{name: "cost_ledger_entry", where: byProject},
	{name: "human_intervention", where: byProject},
	{name: "reference_item", where: byProject},
	{name: "branch", where: byProject},
	{name: "branch_version", where: `branch_id IN (SELECT id FROM branch WHERE project_id = :pid)`},
	{name: "brand_constraint", where: byProject},
	{name: "note", where: byProject, detach: []string{"origin_id"}},
	{name: "note_version", where: `note_id IN (SELECT id FROM note WHERE project_id = :pid)`},
	{name: "note_promotion", where: `note_version_id IN (SELECT v.id FROM note_version v JOIN note n ON n.id = v.note_id WHERE n.project_id = :pid)`},
	{name: "review_request", where: byProject},
	{name: "review_response", where: `review_request_id IN (SELECT id FROM review_request WHERE project_id = :pid)`},
	{name: "review_comment", where: `review_response_id IN (SELECT s.id FROM review_response s JOIN review_request q ON q.id = s.review_request_id WHERE q.project_id = :pid)`},
}

// unbundledTables are the tables a bundle leaves out, with the reason.
var unbundledTables = map[string]string{ // #nosec G101 -- table names with the reason, not credentials
	"series":                    "shared by the Episodes of a Series, not part of one Project; an exported Project is detached from it",
	"series_version":            "the version history of a Series (see series)",
	"brand_template":            "shared by every Project (a saved set of Brand Constraints); an applied template lives on as the Project's own brand_constraint rows",
	"brand_template_constraint": "the constraints of a Brand Template (see brand_template)",
	"job":                       "background job queue: runtime state of this instance (a progress card whose job is gone is shown as finished)",
	"goose_db_version":          "migration bookkeeping of this instance",
	"sqlite_sequence":           "SQLite internal",
	"review_inbox":              "the Review Packages this instance imported as a reviewer (spec/spec.md §27.1): another team's videos, not part of any Project of this instance",
	"review_inbox_comment":      "the reviewer's comments on review_inbox",
	"comfy_workflow":            "ComfyUI workflows are global, shared by every Project (spec/architecture/comfy/comfy-workflows.md): a bundle does not embed them, and an import warns about the ones its Takes and Locks name that this instance lacks (MissingWorkflows)",
	"comfy_workflow_version":    "the version history of a ComfyUI workflow (see comfy_workflow)",
	"mcp_token":                 "the MCP server's bearer tokens (spec/architecture/mcp.md): credentials of this instance, never to leave it in a bundle",
	"mcp_job_history":           "the history of the long operations the MCP server started (spec/architecture/mcp.md §6): operational records of this instance, never to leave it in a bundle",
}

// Repository reads the Project Data Model of a Project out of the database and writes it into
// a database (spec/requirements/non-functional.md §8). All writes go through DB.Write.
type Repository struct {
	db *repository.DB
}

// New returns a Repository backed by db.
func New(db *repository.DB) *Repository { return &Repository{db: db} }

// Export returns every row of the Project in one consistent snapshot, or repository.ErrNotFound.
func (r *Repository) Export(ctx context.Context, projectID string) (domain.BundleData, error) {
	tx, err := r.db.Reader().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.BundleData{}, fmt.Errorf("begin export: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := repository.Exists(ctx, tx, `SELECT 1 FROM project WHERE id = ?`, projectID); err != nil {
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
		for _, c := range t.detach {
			if _, ok := row[c]; ok {
				row[c] = nil
			}
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// Import writes the Project into the database in one transaction. It returns repository.ErrConflict when the
// database already has a Project with that ID and ErrInvalid (nothing written) when the data
// is not importable: an unknown table or column, a value of the wrong type, a row of another
// Project, or a row the schema or a foreign key rejects. Any other failure (a full disk, a locked
// database, a cancelled context) is not the bundle's fault and is returned as an internal error.
func (r *Repository) Import(ctx context.Context, data domain.BundleData) error {
	if err := checkBundle(data); err != nil {
		return err
	}
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := repository.Exists(ctx, tx, `SELECT 1 FROM project WHERE id = ?`, data.ProjectID); err == nil {
			return repository.ErrConflict
		} else if !errors.Is(err, repository.ErrNotFound) {
			return err
		}
		// Rows refer to each other in cycles (shot.selected_take_id, take, ...): check the foreign
		// keys once, at the end, instead of per row.
		if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
			return err
		}
		for _, t := range data.Tables {
			if err := insertBundleTable(ctx, tx, t); err != nil {
				return err
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
			return fmt.Errorf("%w: %s refers to a missing %s", ErrInvalid, table, parent)
		}
		if err := violations.Err(); err != nil {
			return err
		}
		if err := checkBundleOwnership(ctx, tx, data); err != nil {
			return err
		}
		return warnMissingWorkflows(ctx, tx, data.ProjectID)
	})
	if errors.Is(err, repository.ErrConflict) || errors.Is(err, ErrInvalid) {
		return err
	}
	if err != nil {
		return fmt.Errorf("import bundle: %w", err)
	}
	return nil
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
			if _, ok := known[c]; !ok {
				return fmt.Errorf("%w: %s: unknown column %q", ErrInvalid, t.Name, c)
			}
			cols = append(cols, c)
		}
		slices.Sort(cols)
		if err := checkBundleColumnTypes(t.Name, known, row); err != nil {
			return err
		}
		args := make([]any, len(cols))
		for i, c := range cols {
			args[i] = bundleArg(row[c])
		}
		q := `INSERT INTO ` + t.Name + ` (` + strings.Join(cols, ", ") + `) VALUES (` + strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ") + `)` //nolint:gosec // the table is one of bundleTables and every column was checked against the schema
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return insertError(t.Name, err)
		}
	}
	return nil
}

// insertError says why an INSERT of a bundle row failed: the schema refused the row (NOT NULL,
// CHECK, UNIQUE, a trigger, a foreign key: a SQLITE_CONSTRAINT) and the bundle is invalid, or the
// database could not take any row (a full disk, a lock, a cancelled context) and the import can be
// retried.
func insertError(table string, err error) error {
	var se *sqlite.Error
	if errors.As(err, &se) && se.Code()&0xff == sqlitelib.SQLITE_CONSTRAINT {
		return fmt.Errorf("%w: %s: %w", ErrInvalid, table, err)
	}
	return fmt.Errorf("insert %s: %w", table, err)
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

// tableColumns maps the name of each column of table to its declared type.
func tableColumns(ctx context.Context, tx *sql.Tx, table string) (map[string]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT name, type FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	cols := map[string]string{}
	for rows.Next() {
		var name, declared string
		if err := rows.Scan(&name, &declared); err != nil {
			return nil, err
		}
		cols[name] = declared
	}
	return cols, rows.Err()
}
