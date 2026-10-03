package repository

import (
	"encoding/json"
	"fmt"
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

// checkBundleColumns holds one row of table to the format of its timestamp and JSON columns.
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
