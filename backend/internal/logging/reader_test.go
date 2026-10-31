package logging_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/ousiassllc/mam/backend/internal/logging"
)

var base = time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)

// line is one line of the JSON log at base+offset (what slog's JSON handler writes).
func line(offset time.Duration, level, msg string, attrs ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `{"time":%q,"level":%q,"msg":%q`, base.Add(offset).Format(time.RFC3339Nano), level, msg)
	for i := 0; i+1 < len(attrs); i += 2 {
		fmt.Fprintf(&b, `,%q:%s`, attrs[i], attrs[i+1])
	}
	return b.String() + "}\n"
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	var data = []byte(content)
	if strings.HasSuffix(name, ".gz") {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		_, err := zw.Write(data)
		require.NoError(t, err)
		require.NoError(t, zw.Close())
		data = buf.Bytes()
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o600))
}

func messages(res logging.Result) []string {
	var out []string
	for _, e := range res.Entries {
		out = append(out, e.Message)
	}
	return out
}

func read(t *testing.T, dir string, q logging.Query) logging.Result {
	t.Helper()
	if q.Limit == 0 {
		q.Limit = 100
	}
	res, err := logging.Reader{Dir: dir}.Read(context.Background(), q)
	require.NoError(t, err)
	return res
}

// rotated builds a log directory with the active file and two rotated generations, one compressed.
func rotated(t *testing.T) string {
	dir := t.TempDir()
	write(t, dir, "mam-2026-10-31T08-00-00.000.log.gz", line(-3*time.Hour, "INFO", "oldest")+line(-2*time.Hour, "ERROR", "old failure", "job_id", `"j-1"`))
	write(t, dir, "mam-2026-10-31T08-59-00.000.log", line(-time.Hour, "WARN", "middle", "project_id", `"p-1"`)+"not json at all\n"+line(-30*time.Minute, "INFO", "middle two"))
	write(t, dir, "mam.log", line(0, "DEBUG", "debug line")+line(time.Minute, "INFO", "newest", "shot_id", `"s-1"`, "n", "7"))
	return dir
}

func TestReader_ReadsEveryGenerationNewestFirst(t *testing.T) {
	res := read(t, rotated(t), logging.Query{})
	assert.Equal(t, []string{"newest", "middle two", "middle", "old failure", "oldest"}, messages(res), "the zero level is INFO: no debug line")
	all := read(t, rotated(t), logging.Query{MinLevel: slog.LevelDebug})
	assert.Equal(t, []string{"newest", "debug line", "middle two", "middle", "old failure", "oldest"}, messages(all))
	assert.False(t, res.Truncated)
	assert.Equal(t, "ERROR", res.Entries[3].Level)
	assert.Equal(t, "j-1", res.Entries[3].Attrs["job_id"])
	assert.True(t, base.Add(time.Minute).Equal(res.Entries[0].Time))
}

func TestReader_FiltersByLevelTimeKeywordAndAttributes(t *testing.T) {
	dir := rotated(t)
	for name, tc := range map[string]struct {
		q    logging.Query
		want []string
	}{
		"level":         {logging.Query{MinLevel: slog.LevelWarn}, []string{"middle", "old failure"}},
		"since":         {logging.Query{Since: base.Add(-time.Hour)}, []string{"newest", "middle two", "middle"}},
		"until":         {logging.Query{Until: base.Add(-time.Hour)}, []string{"old failure", "oldest"}},
		"range":         {logging.Query{Since: base.Add(-2 * time.Hour), Until: base.Add(-30 * time.Minute)}, []string{"middle", "old failure"}},
		"keyword":       {logging.Query{Keyword: "MIDDLE"}, []string{"middle two", "middle"}},
		"keyword value": {logging.Query{Keyword: "p-1"}, []string{"middle"}},
		"keyword key":   {logging.Query{Keyword: "shot_id"}, []string{"newest"}},
		"attribute":     {logging.Query{Attrs: map[string]string{"job_id": "j-1"}}, []string{"old failure"}},
		"number":        {logging.Query{Attrs: map[string]string{"shot_id": "s-1", "n": "7"}}, []string{"newest"}},
		"both":          {logging.Query{Attrs: map[string]string{"shot_id": "s-1", "n": "8"}}, nil},
		"no match":      {logging.Query{Keyword: "nothing like this"}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, messages(read(t, dir, tc.q)))
		})
	}
}

func TestReader_LimitKeepsTheNewestAndSaysThereIsMore(t *testing.T) {
	dir := rotated(t)
	res := read(t, dir, logging.Query{Limit: 4, MinLevel: slog.LevelDebug})
	assert.Equal(t, []string{"newest", "debug line", "middle two", "middle"}, messages(res))
	assert.True(t, res.Truncated)

	// Continuing from the oldest returned entry's time gives the rest.
	res = read(t, dir, logging.Query{Limit: 4, MinLevel: slog.LevelDebug, Until: res.Entries[3].Time})
	assert.Equal(t, []string{"old failure", "oldest"}, messages(res))
	assert.False(t, res.Truncated)

	one := read(t, dir, logging.Query{Limit: 1})
	assert.Equal(t, []string{"newest"}, messages(one))
	_, err := logging.Reader{Dir: dir}.Read(context.Background(), logging.Query{})
	assert.Error(t, err, "a limit is required")
}

func TestReader_TheSizeBoundCutsTheAnswer(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := range 20 {
		b.WriteString(line(time.Duration(i)*time.Second, "INFO", "entry", "pad", fmt.Sprintf("%q", strings.Repeat("x", 500))))
	}
	write(t, dir, "mam.log", b.String())
	res := read(t, dir, logging.Query{MaxBytes: 2000})
	assert.Greater(t, len(res.Entries), 0)
	assert.Less(t, len(res.Entries), 20)
	assert.True(t, res.Truncated)
}

func TestReader_SkipsBadLinesCutOffFilesAndLongStrings(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("あ", 5000)
	write(t, dir, "mam.log", line(0, "INFO", "first")+`{"time":"yesterday","level":"INFO","msg":"bad time"}`+"\n"+`{"half`+"\n"+
		line(time.Second, "INFO", long)+line(2*time.Second, "INFO", "last"))
	res := read(t, dir, logging.Query{})
	require.Equal(t, 3, len(res.Entries))
	assert.Equal(t, "last", res.Entries[0].Message)
	assert.Less(t, len([]rune(res.Entries[1].Message)), 2100, "a long message is cut")
	assert.True(t, strings.HasSuffix(res.Entries[1].Message, "(truncated)"))

	// A generation whose gzip is cut off still gives what was readable, and a stray file is ignored.
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write([]byte(line(-time.Hour, "INFO", "from the cut generation")))
	_ = zw.Flush() // no Close: no trailer
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mam-2026-10-31T07-00-00.000.log.gz"), buf.Bytes(), 0o600))
	write(t, dir, "mam-notatime.log", line(-time.Hour, "INFO", "not a generation"))
	write(t, dir, "other.log", line(-time.Hour, "INFO", "other file"))
	res = read(t, dir, logging.Query{})
	require.Len(t, res.Entries, 4)
	assert.Equal(t, "last", res.Entries[0].Message)
	assert.Equal(t, "first", res.Entries[2].Message)
	assert.Equal(t, "from the cut generation", res.Entries[3].Message)
}

func TestReader_AMissingOrEmptyDirectoryHasNoEntries(t *testing.T) {
	assert.Empty(t, read(t, filepath.Join(t.TempDir(), "nope"), logging.Query{}).Entries)
	assert.Empty(t, read(t, t.TempDir(), logging.Query{}).Entries)
}

func TestReader_FlattensGroupsAndHonoursTheContext(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "mam.log", line(0, "INFO", "grouped", "req", `{"id":"r-1","n":3}`, "list", `["a","b"]`))
	res := read(t, dir, logging.Query{Attrs: map[string]string{"req.id": "r-1"}})
	require.Len(t, res.Entries, 1)
	assert.EqualValues(t, "3", fmt.Sprint(res.Entries[0].Attrs["req.n"]))
	assert.Equal(t, `["a","b"]`, res.Entries[0].Attrs["list"])

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := logging.Reader{Dir: dir}.Read(ctx, logging.Query{Limit: 1})
	assert.ErrorIs(t, err, context.Canceled)
}

// What logging.New writes is what the reader reads, including the rotation limit of lumberjack.
func TestReader_ReadsWhatTheLoggerWrites(t *testing.T) {
	dir := t.TempDir()
	logger, closer, err := logging.New(dir, slog.LevelDebug, nil)
	require.NoError(t, err)
	logger.Info("project opened", "project_id", "p-9", "count", 2)
	logger.With("job_id", "j-9").Error("job failed", "err", "boom")
	require.NoError(t, closer.Close())

	res := read(t, dir, logging.Query{Attrs: map[string]string{"job_id": "j-9"}})
	require.Len(t, res.Entries, 1)
	assert.Equal(t, "job failed", res.Entries[0].Message)
	assert.Equal(t, "boom", res.Entries[0].Attrs["err"])
	assert.Equal(t, []string{"job failed", "project opened"}, messages(read(t, dir, logging.Query{})))
	assert.Equal(t, "2", fmt.Sprint(read(t, dir, logging.Query{MinLevel: slog.LevelInfo, Keyword: "opened"}).Entries[0].Attrs["count"]))
}

// The names lumberjack gives the generations it rotates and compresses are the names the reader lists.
func TestReader_ReadsGenerationsRotatedByLumberjack(t *testing.T) {
	dir := t.TempDir()
	file := &lumberjack.Logger{Filename: filepath.Join(dir, logging.FileName), MaxSize: 1, MaxBackups: 5, Compress: true}
	logger := slog.New(slog.NewJSONHandler(file, nil))
	logger.Info("before the first rotation", "job_id", "j-r")
	require.NoError(t, file.Rotate())
	logger.Info("after the rotation")
	require.NoError(t, file.Close())

	deadline := time.Now().Add(10 * time.Second)
	for { // lumberjack compresses in the background
		gz, _ := filepath.Glob(filepath.Join(dir, "mam-*.log.gz"))
		if len(gz) == 1 {
			break
		}
		require.True(t, time.Now().Before(deadline), "lumberjack did not compress the rotated files")
		time.Sleep(20 * time.Millisecond)
	}
	res := read(t, dir, logging.Query{})
	assert.Equal(t, []string{"after the rotation", "before the first rotation"}, messages(res))
	assert.Equal(t, []string{"before the first rotation"}, messages(read(t, dir, logging.Query{Attrs: map[string]string{"job_id": "j-r"}})))
}
