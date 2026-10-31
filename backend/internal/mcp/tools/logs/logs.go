// Package logs holds get_app_logs, which reads the mam log (<log dir>/mam.log and its rotated generations)
// so that an agent can follow a failure to its cause: an `internal` tool error keeps its cause in the log
// only. The tool is read-only and never returns a secret: the keys in the key store and the shapes of the
// credentials mam handles are hidden (logging.Redact).
package logs

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/logging"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// The limits of one call.
const (
	DefaultLimit = 100
	MaxLimit     = 500
	maxAttrs     = 10
)

// Register adds get_app_logs. The log directory is the Kit's (r.LogDir); the service layer is not used.
func Register(r *mcpkit.Registrar, _ *service.Services) {
	mcpkit.Read(r, "get_app_logs",
		"mam のアプリログを新しい順に返す（ローテーション済みの世代も読む）。level（最低レベル）・since / until・keyword・attrs（project_id・shot_id・take_id・job_id・error_id などの属性）で絞り込む。internal エラーの原因は error_id（エラーの文面と get_job / list_jobs に出る）を attrs に渡して探す。API キー・トークンなどの秘密は返さない / Read the mam application log, newest first, filtered by level, time range, keyword and attributes.",
		func(ctx context.Context, in Input) (Output, error) { return read(ctx, r.LogDir, r.Credentials, in) })
}

// Input is the filter. Every field is optional; the filters are combined with AND.
type Input struct {
	Level   string            `json:"level,omitempty" jsonschema:"返す最低レベル: debug / info / warn / error（省略は info。debug 行はログのレベルを下げて起動したときだけある）"`
	Since   string            `json:"since,omitempty" jsonschema:"この時刻以降（RFC 3339。例 2026-10-31T09:00:00+09:00）"`
	Until   string            `json:"until,omitempty" jsonschema:"この時刻より前（RFC 3339）。続きを読むには前回の next_until を渡す"`
	Keyword string            `json:"keyword,omitempty" jsonschema:"メッセージ・属性名・属性値に含まれる文字（大文字小文字を区別しない）"`
	Attrs   map[string]string `json:"attrs,omitempty" jsonschema:"属性の絞り込み。キーと値の組がすべて一致する行だけ（値は文字列。例 {\"job_id\":\"job_01…\",\"error_id\":\"err_…\"}。グループの属性は a.b）"`
	Limit   int               `json:"limit,omitempty" jsonschema:"最大件数（省略は 100、上限 500）"`
}

// Output is the matching lines, newest first.
type Output struct {
	Entries   []logging.Entry `json:"entries" jsonschema:"ログの行（新しい順）。time・level・msg・attrs"`
	Truncated bool            `json:"truncated" jsonschema:"件数かサイズの上限で、まだ一致する古い行がある"`
	NextUntil string          `json:"next_until,omitempty" jsonschema:"truncated のとき、続きを読む until（返した行のうち最も古い時刻）"`
}

func read(ctx context.Context, dir string, keys credential.Store, in Input) (Output, error) {
	q, err := in.query()
	if err != nil {
		return Output{}, err
	}
	if dir == "" {
		return Output{}, &mcpkit.ToolError{Code: mcpkit.CodeUnavailable, Message: "ログのディレクトリが設定されていません", Status: http.StatusServiceUnavailable}
	}
	q.Secrets = storedKeys(keys)
	res, err := logging.Reader{Dir: dir}.Read(ctx, q)
	if err != nil {
		return Output{}, err
	}
	out := Output{Entries: res.Entries, Truncated: res.Truncated}
	if out.Entries == nil {
		out.Entries = []logging.Entry{}
	}
	if res.Truncated && len(res.Entries) > 0 {
		out.NextUntil = res.Entries[len(res.Entries)-1].Time.Format(time.RFC3339Nano)
	}
	return out, nil
}

// storedKeys are the API keys in the key store (and the Hugging Face token): the reader hides them wherever a
// line has them, in whatever shape. A key that cannot be read is skipped.
func storedKeys(keys credential.Store) []string {
	if keys == nil {
		return nil
	}
	var out []string
	for _, p := range service.CredentialProviders {
		out = appendKey(out, keys, p.ID)
	}
	return appendKey(out, keys, modelmanager.HuggingFaceProvider)
}

func appendKey(out []string, keys credential.Store, provider string) []string {
	if key, err := keys.Get(provider); err == nil && key != "" {
		out = append(out, key)
	}
	return out
}

func (in Input) query() (logging.Query, error) {
	q := logging.Query{Keyword: in.Keyword, Attrs: in.Attrs, Limit: in.Limit}
	if q.Limit == 0 {
		q.Limit = DefaultLimit
	}
	if q.Limit < 1 || q.Limit > MaxLimit {
		return q, invalid("limit", "1 以上 500 以下で指定してください")
	}
	switch strings.ToLower(strings.TrimSpace(in.Level)) {
	case "", "info":
		q.MinLevel = slog.LevelInfo
	case "debug":
		q.MinLevel = slog.LevelDebug
	case "warn", "warning":
		q.MinLevel = slog.LevelWarn
	case "error":
		q.MinLevel = slog.LevelError
	default:
		return q, invalid("level", "debug / info / warn / error のどれかを指定してください")
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
	if len(q.Attrs) > maxAttrs {
		return q, invalid("attrs", "属性の絞り込みは 10 個までです")
	}
	for k := range q.Attrs {
		if strings.TrimSpace(k) == "" {
			return q, invalid("attrs", "属性のキーが空です")
		}
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
