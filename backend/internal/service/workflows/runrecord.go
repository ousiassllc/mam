package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/progress"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
)

// runTracker holds the progress of the runs that are running in this process.
type runTracker struct {
	mu  sync.Mutex
	now map[string]progress.Update
}

func (t *runTracker) set(id string, u progress.Update) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.now == nil {
		t.now = map[string]progress.Update{}
	}
	t.now[id] = u
}

func (t *runTracker) get(id string) (progress.Update, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	u, ok := t.now[id]
	return u, ok
}

func (t *runTracker) drop(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.now, id)
}

func extensionOf(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "video/quicktime":
		return ".mov"
	}
	return ".png"
}

func writeRun(dir string, rec RunRecord) error {
	data, err := json.MarshalIndent(rec, "", " ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(filepath.Join(dir, "run.json"), data, 0o600)
}

// Run reads the record of a run, with the progress of one that is still running. A record left running by an
// earlier mam process is a failed run (its ComfyUI job has no owner any more).
func (s *Service) Run(_ context.Context, id string) (RunRecord, error) {
	if !domainID(id) {
		return RunRecord{}, ErrNotFound
	}
	data, err := os.ReadFile(filepath.Join(s.RunsDir, id, "run.json")) //nolint:gosec // id is checked: a UUID
	if errors.Is(err, fs.ErrNotExist) {
		return RunRecord{}, ErrNotFound
	}
	if err != nil {
		return RunRecord{}, err
	}
	var rec RunRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return RunRecord{}, fmt.Errorf("read the record of run %s: %w", id, err)
	}
	if rec.Status == RunRunning {
		if rec.BootID != s.bootID {
			rec.Status, rec.Error = RunFailed, "mam が再起動したため、この実行は中断されました。もう一度実行してください"
		} else if u, ok := s.runs.get(id); ok {
			rec.Percent, rec.Message = u.Percent, u.Message
		}
	}
	return rec, nil
}

// RunFile is the result file of a succeeded run.
func (s *Service) RunFile(ctx context.Context, id string) (path, mime string, err error) {
	rec, err := s.Run(ctx, id)
	if err != nil {
		return "", "", err
	}
	if rec.Status != RunSucceeded || rec.File == "" {
		return "", "", ErrNotFound
	}
	return filepath.Join(s.RunsDir, id, rec.File), rec.MimeType, nil
}

func domainID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, r := range id {
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if r != '-' {
				return false
			}
		case (r < '0' || r > '9') && (r < 'a' || r > 'f'):
			return false
		}
	}
	return true
}

// pruneRuns deletes the results older than runKeep.
func (s *Service) pruneRuns() {
	entries, err := os.ReadDir(s.RunsDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && e.IsDir() && s.Now().Sub(info.ModTime()) > runKeep {
			_ = os.RemoveAll(filepath.Join(s.RunsDir, e.Name()))
		}
	}
}
