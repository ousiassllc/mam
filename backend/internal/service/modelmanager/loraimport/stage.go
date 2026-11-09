package loraimport

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/lorafile"
)

// Stage checks the file the reader holds (declared is the size the request announced, 0 when not known) and keeps it in the imports
// folder until Commit or Discard. Nothing is placed in the loras folder. A refusal is an *modelsettings.InvalidError whose message says
// what is wrong: the name, the size, the format, not a LoRA, the room on the disk, too many files waiting.
func (s *Service) Stage(ctx context.Context, originalName string, declared int64, r io.Reader) (Staged, error) {
	if !s.Supported {
		return Staged{}, invalid("同梱の ComfyUI が使えない環境のため、LoRA を追加できません（外部の ComfyUI の loras フォルダに直接置いてください）")
	}
	name, renamed, err := lorafile.CleanName(originalName)
	if err != nil {
		return Staged{}, refusal(err)
	}
	if declared > s.maxBytes() {
		return Staged{}, s.tooLarge(declared)
	}
	if err := s.CheckRoom(declared); err != nil {
		return Staged{}, err
	}
	token, err := newToken()
	if err != nil {
		return Staged{}, err
	}
	if err := s.reserve(); err != nil {
		return Staged{}, err
	}
	file := filepath.Join(s.Layout.ImportsDir(), token+".part")
	sum, size, err := s.write(ctx, file, r)
	if err != nil {
		return Staged{}, err
	}
	info, err := lorafile.Inspect(file)
	if err != nil {
		_ = os.Remove(file)
		return Staged{}, refusal(err)
	}
	st := Staged{Token: token, OriginalName: originalName, Name: name, Renamed: renamed, Size: size, SHA256: sum, Meta: info.Metadata}
	st.Conflict = s.conflictOf(name, sum, size)
	s.mu.Lock()
	s.staged[token] = &entry{Staged: st, file: file, created: s.Now()}
	s.mu.Unlock()
	return st, nil
}

// CheckRoom refuses a file that does not fit the disk (the file is kept once in the imports folder and then renamed, so one copy is needed).
func (s *Service) CheckRoom(size int64) error {
	if size <= 0 {
		return nil
	}
	if free := s.Free(s.Layout.DataDir()); uint64(size) > free {
		return invalid("空き容量が足りません（必要 %s、空き %s、場所 %s）", fetch.HumanBytes(uint64(size)), fetch.HumanBytes(free), s.Layout.DataDir())
	}
	return nil
}

// reserve makes the imports folder and drops the files that waited too long; it refuses when too many wait.
func (s *Service) reserve() error {
	s.mu.Lock()
	s.dropStaleLocked()
	n := len(s.staged)
	s.mu.Unlock()
	if n >= maxStaged {
		return invalid("確認待ちのファイルが %d 個あります。先にそれを追加するか取り消してください", n)
	}
	if err := s.Layout.CheckWritable(); err != nil {
		return err
	}
	return os.MkdirAll(s.Layout.ImportsDir(), 0o750)
}

func (s *Service) dropStaleLocked() {
	for token, e := range s.staged {
		if s.Now().Sub(e.created) > staleAfter {
			_ = os.Remove(e.file)
			delete(s.staged, token)
		}
	}
}

// readFailed marks an error of the source (the upload broke off) as against an error of the disk.
type readFailed struct{ err error }

func (e readFailed) Error() string { return e.err.Error() }
func (e readFailed) Unwrap() error { return e.err }

type source struct {
	ctx context.Context
	r   io.Reader
}

func (s source) Read(p []byte) (int, error) {
	if err := s.ctx.Err(); err != nil {
		return 0, readFailed{err}
	}
	n, err := s.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		err = readFailed{err}
	}
	return n, err
}

// write streams r into file (created new, owner only) and returns its SHA-256 and size. A file larger than lorafile.MaxBytes is cut off
// at once, whatever the request announced. On any failure the file is removed.
func (s *Service) write(ctx context.Context, file string, r io.Reader) (sum string, size int64, err error) {
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- below the imports folder, named by a random token
	if err != nil {
		return "", 0, err
	}
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(file)
		}
	}()
	h := sha256.New()
	size, err = io.Copy(io.MultiWriter(f, h), io.LimitReader(source{ctx, r}, s.maxBytes()+1))
	var rf readFailed
	var big *http.MaxBytesError
	switch {
	case errors.As(err, &big):
		return "", 0, s.tooLarge(0)
	case errors.As(err, &rf):
		return "", 0, invalid("ファイルを最後まで受け取れませんでした（送信が途中で切れました）。もう一度選んでください")
	case err != nil:
		return "", 0, invalid("ファイルを書き込めませんでした。空き容量と書き込み権限を確かめてください: %v", err)
	case size > s.maxBytes():
		return "", 0, s.tooLarge(0)
	}
	if err = f.Sync(); err != nil {
		return "", 0, err
	}
	if err = f.Close(); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

func (s *Service) maxBytes() int64 {
	if s.MaxBytes > 0 {
		return s.MaxBytes
	}
	return lorafile.MaxBytes
}

// tooLarge is the refusal of a file over the limit (size 0: the size is not known, the file was cut off at the limit).
func (s *Service) tooLarge(size int64) error {
	if size > 0 {
		return invalid("ファイルが大きすぎます（%s。上限 %s）。LoRA ではなくチェックポイントなどの大きなモデルではありませんか", lorafile.SizeText(size), lorafile.SizeText(s.maxBytes()))
	}
	return invalid("ファイルが大きすぎます（上限 %s）。LoRA ではなくチェックポイントなどの大きなモデルではありませんか", lorafile.SizeText(s.maxBytes()))
}

func newToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
