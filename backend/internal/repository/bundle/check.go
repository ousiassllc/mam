package bundle

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/bundlepath"
)

// checkBundle verifies what can be verified without the database: known tables, one Project row
// with the bundle's ID, that every row ID is path-safe, and that every row that names a Project
// names this one.
func checkBundle(data domain.BundleData) error {
	if !bundlepath.ValidID(data.ProjectID) {
		return fmt.Errorf("%w: project id", ErrInvalid)
	}
	known := map[string]bool{}
	detachColumns := map[string][]string{}
	noID := map[string]bool{}
	for _, t := range bundleTables {
		known[t.name] = true
		detachColumns[t.name] = t.detach
		noID[t.name] = t.noID
	}
	seen := map[string]bool{}
	for _, t := range data.Tables {
		if !known[t.Name] || seen[t.Name] {
			return fmt.Errorf("%w: table %q", ErrInvalid, t.Name)
		}
		seen[t.Name] = true
		for _, row := range t.Rows {
			// Row IDs are joined into file paths (take previews, audio directories, edit previews), so every
			// one is held to the Project ID's character set. A row without an id would be inserted with a NULL
			// primary key (SQLite does not make "TEXT PRIMARY KEY" NOT NULL), which no ID check or ownership
			// query sees, so every table with an id column requires one.
			if !noID[t.Name] {
				if s, isString := row["id"].(string); !isString || !bundlepath.ValidID(s) {
					return fmt.Errorf("%w: %s has a missing or unsafe id", ErrInvalid, t.Name)
				}
			}
			if t.Name == "project" && row["id"] != data.ProjectID {
				return fmt.Errorf("%w: a project row of another project", ErrInvalid)
			}
			if pid, ok := row["project_id"]; (ok || noID[t.Name]) && pid != data.ProjectID {
				return fmt.Errorf("%w: %s has a row of another project", ErrInvalid, t.Name)
			}
			if t.Name == "chat_message" && row["kind"] == string(domain.KindTake) {
				if err := checkTakeCardURLs(row); err != nil {
					return err
				}
			}
			if err := checkBundleColumns(t.Name, row); err != nil {
				return err
			}
			for _, c := range detachColumns[t.Name] {
				if row[c] != nil {
					return fmt.Errorf("%w: %s.%s points outside the bundle", ErrInvalid, t.Name, c)
				}
			}
		}
	}
	for _, t := range data.Tables {
		if t.Name == "project" && len(t.Rows) == 1 {
			return nil
		}
	}
	return fmt.Errorf("%w: exactly one project row is required", ErrInvalid)
}

// checkTakeCardURLs refuses a take card whose video or poster is not a same-origin path: the
// player would load it from another host.
func checkTakeCardURLs(row domain.BundleRow) error {
	content, _ := row["content"].(string)
	body := domain.DecodeTakeCardBody(content)
	for _, u := range []string{body.VideoURL, body.PosterURL} {
		if u != "" && !domain.IsLocalPath(u) {
			return fmt.Errorf("%w: a take card points outside the app", ErrInvalid)
		}
	}
	return nil
}

// checkBundleOwnership runs inside the import transaction, after the rows are in. The Project is new
// to the database, so every row that belongs to it is found by its table's where condition. Every
// inserted row must be found that way (a Shot whose scene_id names another Project's Scene hangs off
// that Project and is not), and every foreign key of those rows must land on a row of the same
// Project: the database-wide foreign_key_check is satisfied by a parent that belongs to someone else.
func checkBundleOwnership(ctx context.Context, tx *sql.Tx, data domain.BundleData) error {
	inserted := make(map[string]int, len(data.Tables))
	for _, t := range data.Tables {
		inserted[t.Name] = len(t.Rows)
	}
	byName := make(map[string]bundleTable, len(bundleTables))
	for _, t := range bundleTables {
		byName[t.name] = t
	}
	pid := sql.Named("pid", data.ProjectID)
	for _, t := range bundleTables {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+t.name+` WHERE `+t.where, pid).Scan(&n); err != nil { //nolint:gosec // name and where are constants of bundleTables
			return err
		}
		if n != inserted[t.name] {
			return fmt.Errorf("%w: %s has a row that hangs off another project", ErrInvalid, t.name)
		}
	}
	for _, t := range bundleTables {
		fks, err := foreignKeys(ctx, tx, t.name)
		if err != nil {
			return err
		}
		for _, fk := range fks {
			inParent := `NULL` // a parent outside the bundle (a Series, a Brand Template, a review inbox) is never one of the Project's
			if parent, ok := byName[fk.parent]; ok {
				inParent = `SELECT id FROM ` + parent.name + ` WHERE id IS NOT NULL AND (` + parent.where + `)` // a NULL in the set would make NOT IN never true
			}
			var n int
			q := `SELECT count(*) FROM ` + t.name + ` WHERE (` + t.where + `) AND ` + fk.column + ` IS NOT NULL AND ` + fk.column + ` NOT IN (` + inParent + `)` //nolint:gosec // names come from bundleTables and the schema
			if err := tx.QueryRowContext(ctx, q, pid).Scan(&n); err != nil {
				return err
			}
			if n > 0 {
				return fmt.Errorf("%w: %s.%s refers to a row outside the project", ErrInvalid, t.name, fk.column)
			}
		}
	}
	return nil
}

type foreignKey struct{ column, parent string }

func foreignKeys(ctx context.Context, tx *sql.Tx, table string) ([]foreignKey, error) {
	rows, err := tx.QueryContext(ctx, `SELECT "from", "table" FROM pragma_foreign_key_list(?)`, table)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []foreignKey
	for rows.Next() {
		var fk foreignKey
		if err := rows.Scan(&fk.column, &fk.parent); err != nil {
			return nil, err
		}
		out = append(out, fk)
	}
	return out, rows.Err()
}
