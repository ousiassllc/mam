package takes

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// TraceOutput is everything that led to a Take (F-27).
type TraceOutput struct {
	ProjectID         string               `json:"project_id"`
	Take              TraceTake            `json:"take"`
	ModelParameters   any                  `json:"model_parameters,omitempty" jsonschema:"保存されているモデルのパラメータ（JSON）"`
	Routing           any                  `json:"routing,omitempty" jsonschema:"動画モデルを選んだ理由（Multi-model routing）。古い Take では省略"`
	PromptVersion     TracePrompt          `json:"prompt_version"`
	WorldStateVersion int                  `json:"world_state_version" jsonschema:"0 は不明、または World State の版がまだ無かった"`
	References        []domain.Reference   `json:"references" jsonschema:"モデルに渡した Reference"`
	Output            *TraceFile           `json:"output,omitempty" jsonschema:"Take のファイル。アーカイブ済みなら省略"`
	Judgements        []domain.JudgeResult `json:"judgements" jsonschema:"Judge 結果（新しい順）"`
	Repairs           []TraceRepair        `json:"repairs" jsonschema:"この Take への Repair（古い順）"`
	ProducedBy        *TraceRepair         `json:"produced_by,omitempty" jsonschema:"この Take を作った Repair。最初の生成なら省略"`
}

// TraceTake is the Take.
type TraceTake struct {
	ID        string    `json:"id"`
	ShotID    string    `json:"shot_id"`
	ModelName string    `json:"model_name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// TracePrompt is the Source Shot Spec and what the Model Adapter made of it.
type TracePrompt struct {
	VersionNumber int       `json:"version_number"`
	SpecSnapshot  any       `json:"spec_snapshot,omitempty"`
	AdapterOutput any       `json:"adapter_output,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// TraceFile is the file a Take produced.
type TraceFile struct {
	AssetID   string `json:"asset_id"`
	MimeType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
}

// TraceRepair is one repair of the Repair Agent.
type TraceRepair struct {
	ID              string    `json:"id"`
	Action          string    `json:"action"`
	Instructions    any       `json:"instructions,omitempty"`
	ResultingTakeID string    `json:"resulting_take_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

func getTrace(svc *service.TraceService) mcpkit.Handler[TakeInput, TraceOutput] {
	return func(ctx context.Context, in TakeInput) (TraceOutput, error) {
		tr, err := svc.Take(ctx, in.TakeID)
		if err != nil {
			return TraceOutput{}, err
		}
		out := TraceOutput{
			ProjectID:       tr.ProjectID,
			Take:            TraceTake{ID: tr.Take.ID, ShotID: tr.Take.ShotID, ModelName: tr.Take.ModelName, Status: string(tr.Take.Status), CreatedAt: tr.Take.CreatedAt},
			ModelParameters: jsonValue(tr.ModelParameters), Routing: asValue(tr.Routing),
			PromptVersion: TracePrompt{
				VersionNumber: tr.PromptVersion.VersionNumber, SpecSnapshot: jsonValue(tr.PromptVersion.SpecSnapshot),
				AdapterOutput: jsonValue(tr.PromptVersion.AdapterOutput), CreatedAt: tr.PromptVersion.CreatedAt,
			},
			WorldStateVersion: tr.WorldStateVersion,
			References:        append([]domain.Reference{}, tr.References...),
			Judgements:        append([]domain.JudgeResult{}, tr.Judgements...),
			Repairs:           make([]TraceRepair, 0, len(tr.Repairs)),
		}
		if o := tr.Output; o != nil {
			out.Output = &TraceFile{AssetID: o.AssetID, MimeType: o.MimeType, SizeBytes: o.SizeBytes}
		}
		for _, rec := range tr.Repairs {
			out.Repairs = append(out.Repairs, repairOf(rec))
		}
		if p := tr.ProducedBy; p != nil {
			rec := repairOf(*p)
			out.ProducedBy = &rec
		}
		return out, nil
	}
}

func repairOf(r domain.RepairRecord) TraceRepair {
	return TraceRepair{ID: r.ID, Action: string(r.Action), Instructions: asValue(r.Instructions), ResultingTakeID: r.ResultingTakeID, CreatedAt: r.CreatedAt}
}

// jsonValue decodes stored JSON into a value the output carries as it is (nil for empty or broken JSON).
func jsonValue(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}

// asValue turns a domain struct into the JSON value it marshals to (nil for a nil pointer).
func asValue(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return jsonValue(raw)
}
