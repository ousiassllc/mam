package jobhistory

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/ousiassllc/mam/backend/internal/repository"
)

type rowScanner interface{ Scan(dest ...any) error }

func args(e Entry) []any {
	return []any{
		e.ID, e.Kind, e.Target, nullString(e.ProjectID), nullString(e.QueueJobID), e.State, e.Message, e.Done, e.Total, nullJSON(e.Result),
		e.ErrorCode, e.ErrorMessage, nullString(e.ErrorID), repository.FormatTime(e.StartedAt), nullTime(e.FinishedAt), nullTime(e.EndedAt),
	}
}

func scan(s rowScanner) (Entry, error) {
	var (
		e                               Entry
		project, queue, result, errorID sql.NullString
		started                         string
		finished, ended                 sql.NullString
	)
	err := s.Scan(&e.ID, &e.Kind, &e.Target, &project, &queue, &e.State, &e.Message, &e.Done, &e.Total, &result,
		&e.ErrorCode, &e.ErrorMessage, &errorID, &started, &finished, &ended)
	if err != nil {
		return Entry{}, err
	}
	e.ProjectID, e.QueueJobID, e.ErrorID = project.String, queue.String, errorID.String
	if result.Valid {
		e.Result = json.RawMessage(result.String)
	}
	if e.StartedAt, err = repository.ParseTime(started); err != nil {
		return Entry{}, err
	}
	if e.FinishedAt, err = parseNull(finished); err != nil {
		return Entry{}, err
	}
	if e.EndedAt, err = parseNull(ended); err != nil {
		return Entry{}, err
	}
	return e, nil
}

func nullString(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

func nullJSON(b json.RawMessage) sql.NullString {
	return sql.NullString{String: string(b), Valid: len(b) > 0}
}

func nullTime(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: repository.FormatTime(*t), Valid: true}
}

func parseNull(s sql.NullString) (*time.Time, error) {
	if !s.Valid {
		return nil, nil
	}
	t, err := repository.ParseTime(s.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
