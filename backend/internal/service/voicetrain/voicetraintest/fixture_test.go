package voicetraintest_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
	"github.com/ousiassllc/mam/backend/internal/service/voicetrain"
	"github.com/ousiassllc/mam/backend/internal/worktrack"
)

type fixture struct {
	t       *testing.T
	db      *repository.DB
	assets  string
	voices  *voicerefsvc.Service
	svc     *voicetrain.Service
	rt      *fakeRuntime
	free    atomic.Uint64
	guard   *guard
	project voicerefsvc.Owner
}

// wavOf is a PCM WAV whose first sample holds its length in seconds, which fakeProbe reads back.
func wavOf(seconds int) []byte {
	var b bytes.Buffer
	data := make([]byte, 64)
	binary.LittleEndian.PutUint16(data, uint16(seconds)) //nolint:gosec // a test length
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+len(data)))
	b.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(8000), uint32(16000), uint16(2), uint16(16)} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(data)))
	b.Write(data)
	return b.Bytes()
}

func file(name string, data []byte) voicerefsvc.File {
	return voicerefsvc.File{Name: name, Size: int64(len(data)), Open: func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }}
}

func newFixture(t *testing.T, mode string) *fixture {
	t.Helper()
	db := testdb.Open(t)
	f := &fixture{t: t, db: db, assets: t.TempDir(), rt: &fakeRuntime{mode: mode, work: filepath.Join(t.TempDir(), "train")}, guard: &guard{}, project: voicerefsvc.ForProject("p1")}
	f.free.Store(1 << 40)
	f.voices = voicerefsvc.New(voicerefsvc.Deps{
		DB: db, Series: repository.NewSeriesRepository(db), Projects: repository.NewProjectRepository(db), World: repository.NewWorldRepository(db),
		AssetsDir: f.assets, Probe: fakeProbe{},
	})
	f.svc = voicetrain.New(voicetrain.Deps{
		Voices: f.voices, Runtime: f.rt, Probe: fakeProbe{}, GPU: f.guard, AssetsDir: f.assets,
		FreeBytes: func(string) (uint64, error) { return f.free.Load(), nil }, Stop: 300 * time.Millisecond,
	})
	t.Cleanup(f.svc.Close)
	f.exec(`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p1', 'auto', 'key_check', 'product_cm', 'balanced')`)
	f.exec(`INSERT INTO character (id, project_id, name) VALUES ('c1', 'p1', 'ユイ')`)
	return f
}

func (f *fixture) exec(q string, args ...any) {
	f.t.Helper()
	require.NoError(f.t, f.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, q, args...)
		return err
	}))
}

// reference gives c1 the reference voice a training needs.
func (f *fixture) reference() {
	f.t.Helper()
	text := "おはよう"
	_, err := f.voices.SetForProject(context.Background(), "p1", "c1", voicerefsvc.Input{File: file("ref.wav", wavOf(5)), Text: &text})
	require.NoError(f.t, err)
}

// clips adds n clips of the seconds each, with a transcript.
func (f *fixture) clips(n, seconds int) {
	f.t.Helper()
	for i := range n {
		_, err := f.svc.AddClip(context.Background(), f.project, "c1", file(fmt.Sprintf("c%d.wav", i), wavOf(seconds)), fmt.Sprintf("これは %d 番目の音声です", i))
		require.NoError(f.t, err)
	}
}

// ready is a Character with a reference voice and a minute of clips.
func (f *fixture) ready() {
	f.t.Helper()
	f.reference()
	f.clips(6, 10)
}

func (f *fixture) wait(id string, want worktrack.State) voicetrain.Status {
	f.t.Helper()
	var st voicetrain.Status
	require.Eventually(f.t, func() bool {
		var ok bool
		st, ok = f.svc.Status(id)
		return ok && st.State == want
	}, 30*time.Second, 20*time.Millisecond, "the training should be %s (is %v)", want, st.State)
	return st
}

// finished waits for the run's own cleanup, which follows the state.
func (f *fixture) finished() {
	f.t.Helper()
	require.Eventually(f.t, func() bool { return !f.svc.Busy() && f.rt.ended.Load() == f.rt.held.Load() }, 10*time.Second, 10*time.Millisecond)
}

func (f *fixture) files(root string) []string {
	f.t.Helper()
	var out []string
	require.NoError(f.t, filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return err
	}))
	return out
}

func (f *fixture) weightDirs() []string {
	f.t.Helper()
	entries, err := os.ReadDir(filepath.Join(f.assets, voiceref.FinetuneDir))
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(f.t, err)
	var out []string
	for _, e := range entries {
		if e.Name() != "_draft" {
			out = append(out, e.Name())
		}
	}
	return out
}

func (f *fixture) reference0() *voiceref.Reference {
	f.t.Helper()
	ref, err := f.voices.Reference(context.Background(), f.project, "c1")
	require.NoError(f.t, err)
	return ref
}

func failureCode(t *testing.T, err error) apperr.Code {
	t.Helper()
	ae, ok := apperr.As(err)
	require.True(t, ok, "the refusal is the common error: %v", err)
	return ae.Code
}
