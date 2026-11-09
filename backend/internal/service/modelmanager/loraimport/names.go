package loraimport

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/hfplan"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/lorafile"
)

// Staged returns the file that waits under the token.
func (s *Service) Staged(token string) (Staged, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropStaleLocked()
	e, ok := s.staged[token]
	if !ok {
		return Staged{}, false
	}
	return e.Staged, true
}

// Discard throws away the file that waits under the token (unknown: nothing happens).
func (s *Service) Discard(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.staged[token]; ok {
		_ = os.Remove(e.file)
		delete(s.staged, token)
	}
}

// conflictOf says whether name is taken in the loras folder (without regard to case: Windows and macOS do not tell the names apart) and
// what could be used instead. Content is compared only when the sizes are equal.
func (s *Service) conflictOf(name, sum string, size int64) *Conflict {
	taken := s.takenName(name)
	if taken == "" {
		return nil
	}
	c := &Conflict{Name: taken, Suggest: s.freeName(name, sum)}
	if st, err := os.Stat(filepath.Join(s.lorasDir(), taken)); err == nil && st.Mode().IsRegular() && st.Size() == size {
		c.Same = fileSum(filepath.Join(s.lorasDir(), taken)) == sum
	}
	return c
}

// takenName is the entry of the loras folder that has the name (any case), "" when there is none.
func (s *Service) takenName(name string) string {
	entries, err := os.ReadDir(s.lorasDir())
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if strings.EqualFold(e.Name(), name) {
			return e.Name()
		}
	}
	return ""
}

// freeName is name with the start of the SHA-256 before the extension, and a number when that is taken as well.
func (s *Service) freeName(name, sum string) string {
	cand := hfplan.SuggestName(name, sum)
	for i := 2; s.takenName(cand) != "" && i < 100; i++ {
		stem := strings.TrimSuffix(hfplan.SuggestName(name, sum), lorafile.Ext)
		cand = fmt.Sprintf("%s-%d%s", stem, i, lorafile.Ext)
	}
	return cand
}

func fileSum(file string) string {
	f, err := os.Open(file) // #nosec G304 -- an entry of the loras folder
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// check validates the metadata the user confirmed and turns it into the record's fields.
