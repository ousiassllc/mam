package logging

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

// backupTimeFormat is how lumberjack writes the time of a rotation into the name of the old file
// (mam-2026-10-31T01-02-03.456.log, then .log.gz once compressed). The time is UTC, since mam does not set
// LocalTime.
const backupTimeFormat = "2006-01-02T15-04-05.000"

// logFile is one generation of the log.
type logFile struct {
	path string
	// rotatedAt is when the file was closed (everything in it is older); zero for the active file.
	rotatedAt time.Time
	gz        bool
}

var (
	backupPrefix = strings.TrimSuffix(FileName, filepath.Ext(FileName)) + "-" // "mam-"
	backupExt    = filepath.Ext(FileName)                                     // ".log"
)

// listFiles returns the generations in dir, newest first: the active file, then the rotated ones by the
// time in their names. A missing directory has none.
func listFiles(dir string) ([]logFile, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read log dir: %w", err)
	}
	var active *logFile
	var rotated []logFile
	plain := map[string]bool{} // the rotated files that are not compressed (yet)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		path := filepath.Join(dir, name)
		if name == FileName {
			active = &logFile{path: path}
			continue
		}
		stamp, gz, ok := strings.Cut(strings.TrimPrefix(name, backupPrefix), backupExt)
		if !strings.HasPrefix(name, backupPrefix) || !ok || (gz != "" && gz != ".gz") {
			continue
		}
		at, err := time.Parse(backupTimeFormat, stamp)
		if err != nil {
			continue
		}
		rotated = append(rotated, logFile{path: path, rotatedAt: at, gz: gz == ".gz"})
		if gz == "" {
			plain[stamp] = true
		}
	}
	// lumberjack compresses a rotated file in the background: while it does, the file and its .gz (possibly
	// half written) both exist. The file is whole, so it wins.
	rotated = slices.DeleteFunc(rotated, func(f logFile) bool { return f.gz && plain[f.rotatedAt.Format(backupTimeFormat)] })
	sort.Slice(rotated, func(i, j int) bool { return rotated[i].rotatedAt.After(rotated[j].rotatedAt) })
	if active != nil {
		return append([]logFile{*active}, rotated...), nil
	}
	return rotated, nil
}

// open returns the content of the file, decompressed. A file that vanished (rotation or the age limit
// removed it after the listing) is reported as os.ErrNotExist. A rotated file that was compressed after the
// listing is read from its .gz.
func (f logFile) open() (io.ReadCloser, error) {
	file, err := os.Open(f.path)
	if os.IsNotExist(err) && !f.gz && !f.rotatedAt.IsZero() {
		f.path, f.gz = f.path+".gz", true
		file, err = os.Open(f.path)
	}
	if err != nil {
		return nil, err
	}
	if !f.gz {
		return file, nil
	}
	zr, err := gzip.NewReader(file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return &gzipFile{Reader: zr, file: file}, nil
}

type gzipFile struct {
	*gzip.Reader
	file *os.File
}

func (g *gzipFile) Close() error {
	err := g.Reader.Close()
	if cerr := g.file.Close(); err == nil {
		err = cerr
	}
	return err
}
