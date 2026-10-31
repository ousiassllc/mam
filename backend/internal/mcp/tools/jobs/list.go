package jobs

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

const listDescription = "長い操作（ジョブ）の履歴を新しい順に返す。再起動後も残る（30 日・10,000 件まで）。kind・state（failed など）・error_code（validation・conflict・budget_exceeded・upstream・internal など）・project_id・since / until で絞り込み、next_cursor で次のページを取る。internal の失敗は error_id を持ち、get_app_logs の attrs に渡すと原因のログ行が見つかる。結果（result）は get_job で / List the history of long operations, newest first, filtered by kind, state, error code, Project and start time; paged with next_cursor."

// The page sizes of list_jobs.
const (
	DefaultLimit = 50
	MaxLimit     = 200
)

// ListInput is the filter and the page. Every field is optional; the filters are combined with AND.
type ListInput struct {
	Kind      string `json:"kind,omitempty" jsonschema:"操作の種類（generate_shot・judge_edit_timeline・export_video など）"`
	State     string `json:"state,omitempty" jsonschema:"状態: queued / running / succeeded / failed / cancelled"`
	ErrorCode string `json:"error_code,omitempty" jsonschema:"失敗のコード: validation / not_found / conflict / locked / forbidden / budget_exceeded / rate_limited / unavailable / upstream / internal"`
	ProjectID string `json:"project_id,omitempty" jsonschema:"対象が属する Project の ID"`
	Since     string `json:"since,omitempty" jsonschema:"この時刻以降に始まったもの（RFC 3339）"`
	Until     string `json:"until,omitempty" jsonschema:"この時刻より前に始まったもの（RFC 3339）"`
	Limit     int    `json:"limit,omitempty" jsonschema:"1 ページの件数（省略は 50、上限 200）"`
	Cursor    string `json:"cursor,omitempty" jsonschema:"前のページの next_cursor（そのまま渡す）"`
}

// ListOutput is one page, newest start first.
type ListOutput struct {
	Jobs       []mcpkit.JobSummary `json:"jobs" jsonschema:"ジョブ（新しく始まった順）。result は含まない。get_job で取る"`
	NextCursor string              `json:"next_cursor,omitempty" jsonschema:"次のページがあるときだけ。cursor にそのまま渡す"`
}

var states = []mcpkit.JobState{mcpkit.JobQueued, mcpkit.JobRunning, mcpkit.JobSucceeded, mcpkit.JobFailed, mcpkit.JobCancelled}

func list(ctx context.Context, jobs *mcpkit.Jobs, in ListInput) (ListOutput, error) {
	q, err := in.query()
	if err != nil {
		return ListOutput{}, err
	}
	page, err := jobs.List(ctx, q)
	if err != nil {
		return ListOutput{}, err
	}
	return ListOutput{Jobs: page.Jobs, NextCursor: page.Next}, nil
}

func (in ListInput) query() (mcpkit.JobQuery, error) {
	q := mcpkit.JobQuery{
		Kind: strings.TrimSpace(in.Kind), State: mcpkit.JobState(strings.TrimSpace(in.State)),
		ErrorCode: strings.TrimSpace(in.ErrorCode), ProjectID: strings.TrimSpace(in.ProjectID), Cursor: in.Cursor, Limit: in.Limit,
	}
	if q.Limit == 0 {
		q.Limit = DefaultLimit
	}
	if q.Limit < 1 || q.Limit > MaxLimit {
		return q, invalid("limit", "1 以上 200 以下で指定してください")
	}
	if q.State != "" && !slices.Contains(states, q.State) {
		return q, invalid("state", "queued / running / succeeded / failed / cancelled のどれかを指定してください")
	}
	if q.ErrorCode != "" && !slices.Contains(mcpkit.ErrorCodes(), q.ErrorCode) {
		return q, invalid("error_code", "validation / not_found / conflict / locked / forbidden / budget_exceeded / rate_limited / unavailable / upstream / internal のどれかを指定してください")
	}
	var err error
	if q.Since, err = parseTime("since", in.Since); err != nil {
		return q, err
	}
	if q.Until, err = parseTime("until", in.Until); err != nil {
		return q, err
	}
	if !q.Since.IsZero() && !q.Until.IsZero() && !q.Since.Before(q.Until) {
		return q, invalid("since", "until より前の時刻を指定してください")
	}
	return q, nil
}

func parseTime(field, s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, invalid(field, "RFC 3339 の時刻（例 2026-10-31T09:00:00+09:00）で指定してください")
	}
	return t, nil
}

func invalid(field, msg string) error { return &service.ValidationError{Field: field, Message: msg} }
