// Package logging builds the structured (slog) logger.
//
// Logs are written only to the local disk (rotated) and stderr; nothing is sent
// to external services (non-functional.md §5 テレメトリ, §7 ログ).
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

// FileName is the active log file inside the log directory.
const FileName = "mam.log"

// Rotation limits for the log file.
const (
	maxSizeMB  = 10
	maxBackups = 5
	maxAgeDays = 30
)

// New returns a JSON logger writing to <dir>/mam.log with size-based rotation,
// mirrored as text to console (skipped when console is nil). Close the returned
// closer on shutdown to flush the file.
func New(dir string, level slog.Level, console io.Writer) (*slog.Logger, io.Closer, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("create log dir: %w", err)
	}
	file := &lumberjack.Logger{
		Filename:   filepath.Join(dir, FileName),
		MaxSize:    maxSizeMB,
		MaxBackups: maxBackups,
		MaxAge:     maxAgeDays,
		Compress:   true,
	}
	opts := &slog.HandlerOptions{Level: level}
	handlers := []slog.Handler{slog.NewJSONHandler(file, opts)}
	if console != nil {
		handlers = append(handlers, slog.NewTextHandler(console, opts))
	}
	return slog.New(slog.NewMultiHandler(handlers...)), file, nil
}
