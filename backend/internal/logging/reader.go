package logging

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Limits of Read: the size of one line it parses, of one string it returns and of the whole answer.
const (
	maxLineBytes     = 1 << 20
	maxStringRunes   = 2000
	DefaultMaxBytes  = 200 << 10
	truncatedSuffix  = "…(truncated)"
	cancelCheckLines = 4096
)

// Query selects log entries.
type Query struct {
	// MinLevel is the lowest level returned (slog.LevelDebug for all).
	MinLevel slog.Level
	// Since is the oldest time returned (inclusive) and Until the end of the range (exclusive); the zero
	// time leaves that side open.
	Since, Until time.Time
	// Keyword is a case-insensitive text that the message or an attribute (key or value) contains.
	Keyword string
	// Attrs are attributes an entry must have, all of them: the key as written in the log line ("job_id";
	// "a.b" for an attribute of a group) and the value as text.
	Attrs map[string]string
	// Secrets are literal values (API keys in the key store) that are hidden wherever they appear, besides
	// the shapes Redact knows. They are hidden before the filters look at a line.
	Secrets []string
	// Limit is the most entries returned (at least 1).
	Limit int
	// MaxBytes bounds the JSON size of the entries returned; zero means DefaultMaxBytes.
	MaxBytes int
}

// Entry is one log line.
type Entry struct {
	Time    time.Time      `json:"time"`
	Level   string         `json:"level"`
	Message string         `json:"msg"`
	Attrs   map[string]any `json:"attrs,omitempty"`
}

// Result is what Read found, newest first.
type Result struct {
	Entries []Entry
	// Truncated reports that more entries match than are returned (the limit or the size bound was hit);
	// the oldest returned entry's time is the Until to continue with.
	Truncated bool
}

// Reader reads the log files in Dir: the active mam.log and the rotated generations, compressed or not.
type Reader struct{ Dir string }

// Read returns the entries that match q, newest first. Lines it cannot parse are skipped. Secrets are
// hidden (Redact) before the filters look at a line, so a filter cannot be used to find out whether a
// secret is in the log.
func (r Reader) Read(ctx context.Context, q Query) (Result, error) {
	if q.Limit < 1 {
		return Result{}, errors.New("logging: limit must be at least 1")
	}
	maxBytes := q.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	files, err := listFiles(r.Dir)
	if err != nil {
		return Result{}, err
	}
	want := q.Limit + 1 // one more than returned tells that there is more
	var found []Entry
	for _, f := range files {
		if !q.Since.IsZero() && !f.rotatedAt.IsZero() && f.rotatedAt.Add(time.Second).Before(q.Since) {
			break // this generation and every older one ended before the range
		}
		batch, err := readFile(ctx, f, q, want-len(found))
		if err != nil {
			return Result{}, err
		}
		found = append(found, batch...)
		if len(found) >= want {
			break
		}
	}
	res := Result{Entries: found}
	if len(res.Entries) > q.Limit {
		res.Entries, res.Truncated = res.Entries[:q.Limit], true
	}
	size := 0
	for i, e := range res.Entries {
		b, _ := json.Marshal(e)
		if size += len(b); size > maxBytes && i > 0 {
			res.Entries, res.Truncated = res.Entries[:i], true
			break
		}
	}
	return res, nil
}

// readFile returns up to n of the newest entries of one file that match q, newest first.
func readFile(ctx context.Context, f logFile, q Query, n int) ([]Entry, error) {
	rc, err := f.open()
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil // rotated away while reading
	}
	if err != nil {
		return nil, fmt.Errorf("open log %s: %w", f.path, err)
	}
	defer func() { _ = rc.Close() }()

	ring := make([]Entry, 0, min(n, 256)) // the last n matches in file order
	next := 0                             // where the next match goes once the ring is full
	lines := newLineReader(rc)
	for count := 0; ; count++ {
		if count%cancelCheckLines == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		line, err := lines.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// A generation that is cut off (a crash, a half-written gzip) still yields what was read.
			break
		}
		e, ok := parse(line, q.Secrets)
		if !ok {
			continue
		}
		if !q.Until.IsZero() && !e.Time.Before(q.Until) {
			break // entries are in time order: nothing later in this file is in range
		}
		if !matches(&e, q) {
			continue
		}
		if len(ring) < n {
			ring = append(ring, e)
		} else {
			ring[next] = e
			next = (next + 1) % n
		}
	}
	if len(ring) == n && next != 0 { // the ring wrapped: put it back in file order
		ring = append(ring[next:], ring[:next]...)
	}
	slices.Reverse(ring)
	return ring, nil
}

// matches applies the filters to an entry whose strings are already redacted.
func matches(e *Entry, q Query) bool {
	if !q.Since.IsZero() && e.Time.Before(q.Since) {
		return false
	}
	if levelOf(e.Level) < q.MinLevel {
		return false
	}
	for k, want := range q.Attrs {
		if got, ok := e.Attrs[k]; !ok || textOf(got) != want {
			return false
		}
	}
	if q.Keyword == "" {
		return true
	}
	needle := strings.ToLower(q.Keyword)
	if strings.Contains(strings.ToLower(e.Message), needle) {
		return true
	}
	keys := make([]string, 0, len(e.Attrs))
	for k := range e.Attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if strings.Contains(strings.ToLower(k), needle) || strings.Contains(strings.ToLower(textOf(e.Attrs[k])), needle) {
			return true
		}
	}
	return false
}

func levelOf(s string) slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return slog.LevelInfo
	}
	return l
}

func textOf(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}

// parse reads one JSON line of slog's JSONHandler: time, level and msg, and everything else as attributes
// (a group's attributes are flattened to "group.key"). ok is false for a line that is not one.
func parse(line []byte, secrets []string) (Entry, bool) {
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.UseNumber()
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		return Entry{}, false
	}
	ts, _ := raw[slog.TimeKey].(string)
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return Entry{}, false
	}
	e := Entry{Time: t, Level: str(raw[slog.LevelKey]), Message: clip(Redact(str(raw[slog.MessageKey]), secrets...))}
	delete(raw, slog.TimeKey)
	delete(raw, slog.LevelKey)
	delete(raw, slog.MessageKey)
	if len(raw) > 0 {
		e.Attrs = map[string]any{}
		flatten(e.Attrs, "", raw, secrets)
	}
	return e, true
}

func flatten(dst map[string]any, prefix string, src map[string]any, secrets []string) {
	for k, v := range src {
		key := prefix + k
		switch x := v.(type) {
		case map[string]any:
			flatten(dst, key+".", x, secrets)
		case string:
			dst[key] = clip(redactAttr(key, x, secrets).(string))
		case []any: // a list is shown as the text it was written as, scanned for secrets (all of it under a secret-like key)
			b, _ := json.Marshal(x)
			dst[key] = clip(redactAttr(key, string(b), secrets).(string))
		default:
			dst[key] = v
		}
	}
}

func str(v any) string { s, _ := v.(string); return s }

// clip bounds a string by its length in characters.
func clip(s string) string {
	if utf8.RuneCountInString(s) <= maxStringRunes {
		return s
	}
	return string([]rune(s)[:maxStringRunes]) + truncatedSuffix
}

// lineReader returns the lines of a file; a line longer than maxLineBytes is skipped.
type lineReader struct{ r *bufio.Reader }

func newLineReader(r io.Reader) *lineReader { return &lineReader{r: bufio.NewReaderSize(r, 64<<10)} }

func (l *lineReader) next() ([]byte, error) {
	var line []byte
	tooLong := false
	for {
		chunk, err := l.r.ReadSlice('\n')
		if !tooLong {
			line = append(line, chunk...)
			if len(line) > maxLineBytes {
				tooLong, line = true, nil
			}
		}
		switch {
		case err == nil:
			if tooLong {
				tooLong = false
				continue // skip the long line, read the next one
			}
			return bytes.TrimRight(line, "\r\n"), nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && len(line) > 0 && !tooLong:
			return line, nil
		default:
			return nil, err
		}
	}
}
