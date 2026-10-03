package repository

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// What the schema leaves open in a bundle row: json_valid() accepts any JSON, and a timestamp
// column is plain TEXT. The readers decode these columns into typed values and parse the
// timestamps, and one row that does not fit breaks every read of the Project (the Project list, the
// Workspace, the Timeline). checkBundle holds the rows to the format the readers expect, so that a
// bad bundle is refused at import instead of leaving a Project that cannot be opened.

// jsonShape reports whether raw (the text of a JSON column) is what the column's reader decodes it
// into.
type jsonShape func(raw string) error

// shapeOf is the shape "decodes into a T". A JSON null decodes into any T and is accepted, as the
// readers accept it.
func shapeOf[T any]() jsonShape {
	return func(raw string) error {
		var v T
		return json.Unmarshal([]byte(raw), &v)
	}
}

// checkpointsShape is a list of known checkpoints: an unknown one would never be asked for.
func checkpointsShape(raw string) error {
	var cps []domain.Checkpoint
	if err := json.Unmarshal([]byte(raw), &cps); err != nil {
		return err
	}
	if i := slices.IndexFunc(cps, func(c domain.Checkpoint) bool { return !c.Valid() }); i >= 0 {
		return fmt.Errorf("unknown checkpoint %q", cps[i])
	}
	return nil
}

// bundleJSONColumns are the JSON columns of the bundled tables the code decodes, with the shape it
// decodes them into. TestBundle_EveryJSONColumnHasAShapeOrIsKeptRaw makes a new json_valid column
// fail until it is listed here or in rawJSONColumns.
var bundleJSONColumns = map[string]map[string]jsonShape{
	"project":                {"custom_checkpoints": checkpointsShape},
	"brief":                  {"content": shapeOf[domain.Brief]()},
	"chat_message":           {"attachment_asset_ids": shapeOf[[]string]()},
	"script_version":         {"content": shapeOf[domain.ScriptDoc]()},
	"shot":                   {"shot_spec": shapeOf[map[string]json.RawMessage]()}, // decodeSpecMap: an object
	"prompt_version":         {"note_refs": shapeOf[[]domain.NoteRef]()},
	"take":                   {"scores": shapeOf[domain.TakeScores]()},
	"judge_result":           {"issues": shapeOf[[]domain.JudgeIssue](), "metrics": shapeOf[domain.JudgeMetrics](), "repair_strategy": shapeOf[domain.RepairStrategy](), "scores": shapeOf[map[domain.EvaluationAxis]float64]()},
	"repair_action":          {"instructions": shapeOf[domain.RepairInstructions]()},
	"world_state_version":    {"state": shapeOf[domain.WorldContext]()},
	"edit_timeline_version":  {"timeline": shapeOf[domain.EditTimeline]()},
	"editor_judge_result":    {"findings": shapeOf[[]domain.EditorFinding]()},
	"audio_timeline_version": {"timeline": shapeOf[domain.AudioTimeline]()},
	"final_qc_result":        {"findings": shapeOf[[]domain.QCFinding]()},
	"reference_item":         {"tags": shapeOf[[]string]()},
	"brand_constraint":       {"value": shapeOf[brandValue]()},
	"note_promotion":         {"proposed_diff": shapeOf[domain.StateDiff](), "locked_conflicts": shapeOf[domain.StateDiff]()},
}

// rawJSONColumns are JSON columns of the bundled tables that the code keeps as they are (or reads
// leniently, losing only a summary): any valid JSON is fine.
var rawJSONColumns = map[string][]string{
	"judge_result":   {"preserve"}, // a row nobody wrote through Save has '{}': read as empty
	"lock":           {"value"},
	"prompt_version": {"shot_spec_snapshot", "model_adapter_output"},
	"repair_shadow":  {"probabilities"},
	"state_diff":     {"diff"},
	"take":           {"model_parameters"}, // another shape only loses the resolution and the length
}

// checkBundleColumns holds one row of table to the format of its timestamp and JSON columns (the
// types of the numeric columns are held in insertBundleTable, which has the schema).
func checkBundleColumns(table string, row domain.BundleRow) error {
	for column, v := range row {
		if v == nil {
			continue // a NULL is the schema's to accept or refuse
		}
		if strings.HasSuffix(column, "_at") {
			s, ok := v.(string)
			if !ok {
				return fmt.Errorf("%w: %s.%s is not a timestamp", ErrInvalidBundle, table, column)
			}
			if _, err := parseTime(s); err != nil {
				return fmt.Errorf("%w: %s.%s: %w", ErrInvalidBundle, table, column, err)
			}
		}
		shape, ok := bundleJSONColumns[table][column]
		if !ok {
			continue
		}
		s, isString := v.(string)
		if !isString {
			return fmt.Errorf("%w: %s.%s is not JSON text", ErrInvalidBundle, table, column)
		}
		if err := shape(s); err != nil {
			return fmt.Errorf("%w: %s.%s is not of the expected shape: %w", ErrInvalidBundle, table, column, err)
		}
	}
	return nil
}

// columnClass is what a column of a declared type holds, by SQLite's rules for the affinity of a
// declared type (https://sqlite.org/datatype3.html#determination_of_column_affinity). Only the
// numeric classes constrain the value: SQLite keeps TEXT in a number column as it is (the schema
// has no CHECK or STRICT), and the readers then fail to Scan it.
type columnClass int

const (
	anyClass columnClass = iota
	integerClass
	numberClass
)

func classOf(declared string) columnClass {
	d := strings.ToUpper(declared)
	switch {
	case strings.Contains(d, "INT"):
		return integerClass
	case strings.Contains(d, "CHAR"), strings.Contains(d, "CLOB"), strings.Contains(d, "TEXT"),
		strings.Contains(d, "BLOB"), d == "":
		return anyClass
	default: // REAL, FLOAT, DOUBLE and NUMERIC affinity
		return numberClass
	}
}

// checkBundleColumnTypes holds each value of row to the declared type of its column (declared maps
// the column names of the table to them, from the schema): an INTEGER column takes an integer and a
// REAL or NUMERIC column takes a number. A NULL is the schema's to accept or refuse.
func checkBundleColumnTypes(table string, declared map[string]string, row domain.BundleRow) error {
	for column, v := range row {
		if v == nil {
			continue
		}
		switch classOf(declared[column]) {
		case integerClass:
			if !isInteger(v) {
				return fmt.Errorf("%w: %s.%s is not an integer", ErrInvalidBundle, table, column)
			}
		case numberClass:
			if !isNumber(v) {
				return fmt.Errorf("%w: %s.%s is not a number", ErrInvalidBundle, table, column)
			}
		}
	}
	return nil
}

// isInteger reports whether v is a whole number that fits an int64: a decoded data.json number
// (json.Number) or a Go integer.
func isInteger(v any) bool {
	switch n := v.(type) {
	case json.Number:
		_, err := n.Int64()
		return err == nil
	case int, int8, int16, int32, int64, uint8, uint16, uint32:
		return true
	}
	return false
}

// isNumber reports whether v is a finite number.
func isNumber(v any) bool {
	switch n := v.(type) {
	case json.Number:
		f, err := n.Float64()
		return err == nil && !math.IsInf(f, 0)
	case float32:
		return !math.IsInf(float64(n), 0) && !math.IsNaN(float64(n))
	case float64:
		return !math.IsInf(n, 0) && !math.IsNaN(n)
	}
	return isInteger(v)
}
