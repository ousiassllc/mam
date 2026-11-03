package voicetrain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/domain/bundlepath"
	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
)

// The amount of data a training takes. They are mam's own limits (spec §16.3), not the vendor's: the vendor's scripts only skip a clip outside 0.6 to 54
// seconds. The lower bound of the total is what the vendor's README says is enough for a likeness ("1 minute of training data"); the upper one
// keeps a training to a time and a disk a PC can spare.
const (
	MinClipSeconds  = 2
	MaxClipSeconds  = 30
	MinTotalSeconds = 60
	MaxTotalSeconds = 30 * 60
	MaxClips        = 200
	MaxClipBytes    = voiceref.MaxBytes
)

// draftDir is the directory below the weights root where the clips of the Characters wait for a training. Its name is not an id of a trained
// voice, so the synthesis server can never be asked for it and the sweep of unused weights leaves it alone.
const draftDir = "_draft"

// Clip is one recording of the speaker with what it says. File is its name in the directory of the draft.
type Clip struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	File       string `json:"file"`
	Text       string `json:"text"`
	DurationMS int    `json:"duration_ms"`
	SizeBytes  int64  `json:"size_bytes"`
}

// Duration is the length of the clip.
func (c Clip) Duration() time.Duration { return time.Duration(c.DurationMS) * time.Millisecond }

type index struct {
	Clips []Clip `json:"clips"`
}

// dir is the absolute directory of the draft of a Character ("" without an assets directory or for an id that is no directory name).
func (s *Service) dir(o voicerefsvc.Owner, characterID string) string {
	if s.AssetsDir == "" || !bundlepath.ValidID(o.ID()) || !bundlepath.ValidID(characterID) {
		return ""
	}
	kind := "project"
	if o.IsSeries() {
		kind = "series"
	}
	return filepath.Join(s.AssetsDir, voiceref.FinetuneDir, draftDir, kind+"-"+o.ID(), characterID)
}

func (s *Service) load(dir string) (index, error) {
	var idx index
	raw, err := os.ReadFile(filepath.Join(dir, "clips.json")) //nolint:gosec // below the assets directory, from ids that were checked
	if errors.Is(err, os.ErrNotExist) {
		return idx, nil
	}
	if err != nil {
		return idx, fmt.Errorf("read the clips: %w", err)
	}
	if err := json.Unmarshal(raw, &idx); err != nil {
		return index{}, fmt.Errorf("decode the clips: %w", err)
	}
	return idx, nil
}

func (s *Service) save(dir string, idx index) error {
	if len(idx.Clips) == 0 {
		return os.RemoveAll(dir)
	}
	raw, err := json.Marshal(idx)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(filepath.Join(dir, "clips.json"), raw, 0o600)
}

// reference is the reference voice of the Character: the training needs it (the synthesis reads the recording of the speaker) and takes its language.
func (s *Service) reference(ctx context.Context, o voicerefsvc.Owner, characterID string) (*voiceref.Reference, error) {
	ref, err := s.Voices.Reference(ctx, o, characterID)
	if err != nil {
		return nil, err
	}
	if ref == nil {
		return nil, conflictf("この登場人物には参照音声がありません。学習した声も、合成のときは参照音声（3〜10 秒の録音）を手がかりに使うので、先に参照音声を設定してください")
	}
	return ref, nil
}

// cleanText is the transcript of a clip: trimmed, a line break read as a space, at most voiceref.MaxTextRunes characters, and none of the
// characters the vendor's list file is split by ("|") or that are not text.
func cleanText(text string) (string, error) {
	text = strings.Join(strings.Fields(strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(text)), " ")
	if text == "" {
		return "", invalidf("書き起こしを入力してください（学習は、音声と、それが言っている内容の組で行います）")
	}
	if utf8.RuneCountInString(text) > voiceref.MaxTextRunes {
		return "", invalidf("書き起こしは %d 文字以内にしてください", voiceref.MaxTextRunes)
	}
	if strings.ContainsFunc(text, func(r rune) bool { return r == '|' || r == '｜' || unicode.IsControl(r) }) {
		return "", invalidf("書き起こしに「|」と制御文字は使えません")
	}
	return text, nil
}

// AddClip checks the recording (its format, its size, its length against the limits, the amount of data with it) and keeps it, with its transcript,
// in the draft of the Character. Nothing stays on disk when it is refused. The clip is spoken in the language of the Character's reference voice.
func (s *Service) AddClip(ctx context.Context, o voicerefsvc.Owner, characterID string, f voicerefsvc.File, text string) (Clip, error) {
	dir := s.dir(o, characterID)
	if dir == "" {
		return Clip{}, ErrNotFound
	}
	if _, err := s.reference(ctx, o, characterID); err != nil {
		return Clip{}, err
	}
	text, err := cleanText(text)
	if err != nil {
		return Clip{}, err
	}
	if s.Probe == nil {
		return Clip{}, conflictf("音声の長さを測る ffprobe が見つからないため、学習に使う音声を受け付けられません。設定の「ffmpeg」からダウンロードするか、インストールしてください")
	}
	ext, err := checkFile(f)
	if err != nil {
		return Clip{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, err := s.load(dir)
	if err != nil {
		return Clip{}, err
	}
	if len(idx.Clips) >= MaxClips {
		return Clip{}, invalidf("学習に使う音声は %d 個までです", MaxClips)
	}
	id := newID()
	name := id + ext
	abs := filepath.Join(dir, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Clip{}, fmt.Errorf("create the directory of the clips: %w", err)
	}
	size, err := writeFile(abs, f)
	if err != nil {
		return Clip{}, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(abs)
			if len(idx.Clips) == 0 {
				_ = os.Remove(dir)
			}
		}
	}()
	secs, err := s.Probe.AudioDuration(ctx, abs)
	if err != nil {
		return Clip{}, invalidf("音声として読み取れませんでした（%s の長さを測れません）", filepath.Base(f.Name))
	}
	if secs < MinClipSeconds || secs > MaxClipSeconds {
		return Clip{}, invalidf("1 つの音声は %d〜%d 秒にしてください（この音声は %.1f 秒です）", MinClipSeconds, MaxClipSeconds, secs)
	}
	clip := Clip{ID: id, Name: filepath.Base(f.Name), File: name, Text: text, DurationMS: int(secs * 1000), SizeBytes: size}
	if total := seconds(idx.Clips) + secs; total > MaxTotalSeconds {
		return Clip{}, invalidf("学習に使う音声は合わせて %d 分までです（この音声を足すと %.1f 分になります）", MaxTotalSeconds/60, total/60)
	}
	idx.Clips = append(idx.Clips, clip)
	if err := s.save(dir, idx); err != nil {
		idx.Clips = idx.Clips[:len(idx.Clips)-1]
		return Clip{}, fmt.Errorf("keep the clip: %w", err)
	}
	keep = true
	return clip, nil
}

// checkFile is the extension of the file when it is a recording of a format the vendor's scripts read (ffmpeg reads them) whose content has the
// signature of that format, and whose size is within MaxClipBytes.
func checkFile(f voicerefsvc.File) (string, error) {
	ext := strings.ToLower(filepath.Ext(filepath.Base(f.Name)))
	format, ok := voiceref.FormatOf(ext)
	if !ok {
		return "", invalidf("音声は %s のファイルを選んでください", strings.Join(voiceref.Extensions(), " "))
	}
	if f.Size <= 0 || f.Open == nil {
		return "", invalidf("ファイルが空です")
	}
	if f.Size > MaxClipBytes {
		return "", invalidf("1 つの音声は %d MB までです", MaxClipBytes>>20)
	}
	rc, err := f.Open()
	if err != nil {
		return "", invalidf("ファイルを読み取れませんでした")
	}
	defer func() { _ = rc.Close() }()
	head := make([]byte, 512)
	n, err := io.ReadFull(rc, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", invalidf("ファイルを読み取れませんでした")
	}
	if !format.Matches(head[:n]) {
		return "", invalidf("拡張子とファイルの内容が一致しません")
	}
	return ext, nil
}

// writeFile writes the content of f to abs (at most MaxClipBytes) and returns its size; nothing is left behind when it fails.
func writeFile(abs string, f voicerefsvc.File) (int64, error) {
	var written int64
	err := fsutil.WriteAtomic(abs, 0o600, func(w io.Writer) error {
		rc, err := f.Open()
		if err != nil {
			return invalidf("ファイルを読み取れませんでした")
		}
		defer func() { _ = rc.Close() }()
		written, err = io.Copy(w, io.LimitReader(rc, MaxClipBytes+1))
		if err == nil && written > MaxClipBytes {
			err = invalidf("1 つの音声は %d MB までです", MaxClipBytes>>20)
		}
		return err
	})
	return written, err
}

func seconds(clips []Clip) float64 {
	var total float64
	for _, c := range clips {
		total += float64(c.DurationMS) / 1000
	}
	return total
}

// Clips lists the clips of the Character's draft in the order they were added.
func (s *Service) Clips(ctx context.Context, o voicerefsvc.Owner, characterID string) ([]Clip, error) {
	if _, err := s.Voices.Reference(ctx, o, characterID); err != nil {
		return nil, err
	}
	dir := s.dir(o, characterID)
	if dir == "" {
		return nil, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, err := s.load(dir)
	return idx.Clips, err
}

// RemoveClips deletes the clip with the id from the draft, or every clip when id is "". A clip that is not there is ErrNotFound.
func (s *Service) RemoveClips(ctx context.Context, o voicerefsvc.Owner, characterID, id string) error {
	if _, err := s.Voices.Reference(ctx, o, characterID); err != nil {
		return err
	}
	dir := s.dir(o, characterID)
	if dir == "" {
		return ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" {
		return os.RemoveAll(dir)
	}
	idx, err := s.load(dir)
	if err != nil {
		return err
	}
	for i, c := range idx.Clips {
		if c.ID != id {
			continue
		}
		idx.Clips = append(idx.Clips[:i:i], idx.Clips[i+1:]...)
		if err := s.save(dir, idx); err != nil {
			return err
		}
		if len(idx.Clips) > 0 {
			_ = os.Remove(filepath.Join(dir, c.File))
		}
		return nil
	}
	return ErrNotFound
}

// ClipPath is the absolute path of the clip's recording, for the screen's player ("" when it is not there).
func (s *Service) ClipPath(ctx context.Context, o voicerefsvc.Owner, characterID, id string) (string, error) {
	clips, err := s.Clips(ctx, o, characterID)
	if err != nil {
		return "", err
	}
	for _, c := range clips {
		if c.ID == id {
			return filepath.Join(s.dir(o, characterID), c.File), nil
		}
	}
	return "", ErrNotFound
}

// problems says why the clips are not the amount of data a training takes (none: they are).
func problems(clips []Clip) []string {
	var out []string
	total := seconds(clips)
	switch {
	case len(clips) == 0:
		out = append(out, fmt.Sprintf("学習に使う音声がありません。%d〜%d 秒の音声を、書き起こしと一緒に、合わせて %d 秒以上（目安は 1 分）足してください", MinClipSeconds, MaxClipSeconds, MinTotalSeconds))
	case total < MinTotalSeconds:
		out = append(out, fmt.Sprintf("音声が合わせて %.0f 秒しかありません。%d 秒以上（目安は 1 分）にしてください", total, MinTotalSeconds))
	case total > MaxTotalSeconds:
		out = append(out, fmt.Sprintf("音声が合わせて %.0f 分あります。%d 分までにしてください", total/60, MaxTotalSeconds/60))
	}
	if len(clips) > MaxClips {
		out = append(out, fmt.Sprintf("音声は %d 個までです", MaxClips))
	}
	return out
}
