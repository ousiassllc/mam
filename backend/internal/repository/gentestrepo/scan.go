package gentestrepo

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/mam/backend/internal/repository"
)

type scanner interface{ Scan(dest ...any) error }

func parse(s string) (time.Time, error) { return repository.ParseTime(s) }

func parseOpt(s sql.NullString) (*time.Time, error) {
	if !s.Valid {
		return nil, nil
	}
	t, err := parse(s.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return repository.ErrNotFound
	}
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

const batchColumns = `id, media, job_id, params, estimated_cost, estimate, created_at, finished_at`

func scanBatch(row scanner) (Batch, error) {
	var (
		b             Batch
		params, estim string
		created       string
		finished      sql.NullString
	)
	if err := row.Scan(&b.ID, &b.Media, &b.JobID, &params, &b.EstimatedCost, &estim, &created, &finished); err != nil {
		return Batch{}, err
	}
	b.Params, b.Estimate = json.RawMessage(params), json.RawMessage(estim)
	var err error
	if b.CreatedAt, err = parse(created); err != nil {
		return Batch{}, err
	}
	if b.FinishedAt, err = parseOpt(finished); err != nil {
		return Batch{}, err
	}
	return b, nil
}

const inputColumns = `id, batch_id, role, file_path, mime_type, size_bytes, origin, position`

func scanInput(row scanner) (Input, error) {
	var (
		in     Input
		origin string
	)
	if err := row.Scan(&in.ID, &in.BatchID, &in.Role, &in.FilePath, &in.MimeType, &in.SizeBytes, &origin, &in.Position); err != nil {
		return Input{}, err
	}
	in.Origin = json.RawMessage(origin)
	return in, nil
}

const resultColumns = `id, batch_id, position, media, model, api_model, prompt, negative_prompt, sent_prompt, negative_mode, params, used_inputs,
	seed, seed_sent, status, error, file_path, mime_type, size_bytes, cost_basis, estimated_cost, actual_cost, favorite, created_at, finished_at`

func scanResult(row scanner) (Result, error) {
	var (
		r                  Result
		params, used       string
		seed               sql.NullInt64
		est, actual        sql.NullFloat64
		seedSent, favorite int
		created            string
		finished           sql.NullString
	)
	err := row.Scan(&r.ID, &r.BatchID, &r.Position, &r.Media, &r.Model, &r.APIModel, &r.Prompt, &r.NegativePrompt, &r.SentPrompt, &r.NegativeMode,
		&params, &used, &seed, &seedSent, &r.Status, &r.Error, &r.FilePath, &r.MimeType, &r.SizeBytes, &r.CostBasis, &est, &actual, &favorite, &created, &finished)
	if err != nil {
		return Result{}, err
	}
	r.Params = json.RawMessage(params)
	if err := json.Unmarshal([]byte(used), &r.UsedInputs); err != nil {
		return Result{}, fmt.Errorf("gentest result %s used_inputs: %w", r.ID, err)
	}
	if seed.Valid {
		r.Seed = &seed.Int64
	}
	if est.Valid {
		r.EstimatedCost = &est.Float64
	}
	if actual.Valid {
		r.ActualCost = &actual.Float64
	}
	r.SeedSent, r.Favorite = seedSent == 1, favorite == 1
	if r.CreatedAt, err = parse(created); err != nil {
		return Result{}, err
	}
	if r.FinishedAt, err = parseOpt(finished); err != nil {
		return Result{}, err
	}
	return r, nil
}

func scanPrompt(row scanner) (Prompt, error) {
	var (
		p             Prompt
		created, used string
	)
	if err := row.Scan(&p.ID, &p.Prompt, &p.NegativePrompt, &p.UseCount, &created, &used); err != nil {
		return Prompt{}, err
	}
	var err error
	if p.CreatedAt, err = parse(created); err != nil {
		return Prompt{}, err
	}
	if p.LastUsedAt, err = parse(used); err != nil {
		return Prompt{}, err
	}
	return p, nil
}

func scanImport(row scanner) (Import, error) {
	var (
		im      Import
		item    sql.NullString
		target  string
		created string
	)
	if err := row.Scan(&im.ID, &im.ResultID, &im.ProjectID, &im.AssetID, &item, &target, &created); err != nil {
		return Import{}, err
	}
	im.ReferenceItemID, im.Target = item.String, json.RawMessage(target)
	var err error
	im.CreatedAt, err = parse(created)
	return im, err
}
