package loraimport

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain/lorafamily"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/lorafile"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

func (m Meta) check() (families []string, words []string, err error) {
	families = lorafamily.Normalize(m.BaseModels)
	if len(families) != len(slices.Compact(slices.Sorted(slices.Values(m.BaseModels)))) {
		return nil, nil, invalid("対応するベースモデルの値が正しくありません")
	}
	if m.Strength != nil && !(*m.Strength > 0 && *m.Strength <= 1) { // false for NaN too, which every comparison passes otherwise
		return nil, nil, invalid("推奨の強さは 0 より大きく 1 以下の数にしてください（生成テストの LoRA の強さの範囲です）")
	}
	if words, err = lorafile.ParseWords(m.TriggerWords); err != nil {
		return nil, nil, invalid("%s", err.Error())
	}
	if n := strings.TrimSpace(m.LicenseName); len([]rune(n)) > lorafile.MaxLicenseRunes || strings.ContainsFunc(n, func(r rune) bool { return r < ' ' || r == 0x7f }) {
		return nil, nil, invalid("ライセンス名は %d 文字以内の 1 行で入力してください", lorafile.MaxLicenseRunes)
	}
	switch m.Commercial {
	case "", "conditional", "no":
	default:
		return nil, nil, invalid("商用利用の値が正しくありません")
	}
	return families, words, nil
}

func recordOf(rel, name string, sum string, size int64, original string, m Meta, families, words []string, now time.Time) store.LocalLoRA {
	return store.LocalLoRA{
		Path: rel, Name: name, OriginalName: original, SHA256: sum, Size: size, BaseModels: families, Strength: m.Strength, TriggerWords: words,
		LicenseName: strings.TrimSpace(m.LicenseName), Commercial: m.Commercial, AddedAt: now.UTC(),
	}
}

// Commit places the staged file into the loras folder under the name (in.Name, or the staged one) and records the metadata. A name that is
// taken is refused: the file there is never overwritten, the refusal suggests a free name, and the staged file stays so that the user can choose another name.
// The file is moved with one rename, so the folder never holds a half-written file.
func (s *Service) Commit(_ context.Context, in Input) (Model, error) {
	families, words, err := in.check()
	if err != nil {
		return Model{}, err
	}
	s.mu.Lock()
	s.dropStaleLocked()
	e, ok := s.staged[in.Token]
	s.mu.Unlock()
	if !ok {
		return Model{}, invalid("確認してから時間がたったか、すでに追加したため、このファイルは残っていません。もう一度ファイルを選んでください")
	}
	name := e.Name
	if n := strings.TrimSpace(in.Name); n != "" {
		name = n
	}
	if !lorafile.ValidName(name) {
		return Model{}, invalid("ファイル名は、英数字と . _ + - だけの、拡張子が .safetensors の名前（120 文字まで）にしてください")
	}
	rel := relOf(name)
	unlock := s.Lock(rel)
	defer unlock()
	if c := s.conflictOf(name, e.SHA256, e.Size); c != nil {
		if c.Same {
			return Model{}, invalid("同じ内容のファイルが、すでに %s として loras フォルダにあります。追加は要りません", c.Name)
		}
		return Model{}, invalid("%s という名前のファイルがすでに loras フォルダにあります（上書きしません）。別の名前（例: %s）を入力して追加してください", c.Name, c.Suggest)
	}
	if err := os.MkdirAll(s.lorasDir(), 0o750); err != nil {
		return Model{}, err
	}
	dest := s.Layout.ModelPath(rel)
	if err := os.Rename(e.file, dest); err != nil {
		return Model{}, fmt.Errorf("loras フォルダへ移せませんでした: %w", err)
	}
	rec := recordOf(rel, name, e.SHA256, e.Size, e.OriginalName, in.Meta, families, words, s.Now())
	if err := s.State.Update(func(st *store.State) {
		if st.LocalLoRAs == nil {
			st.LocalLoRAs = map[string]store.LocalLoRA{}
		}
		st.LocalLoRAs[rel] = rec
	}); err != nil {
		// Not recorded: leave nothing in the loras folder. The file goes back to the staged place so that the user can commit again without
		// uploading it once more; when it cannot, the staged entry is dropped (its file is gone) so that a retry says so instead of failing.
		if rerr := os.Rename(dest, e.file); rerr != nil {
			_ = os.Remove(dest)
			s.mu.Lock()
			delete(s.staged, in.Token)
			s.mu.Unlock()
		}
		return Model{}, err
	}
	s.mu.Lock()
	delete(s.staged, in.Token)
	s.mu.Unlock()
	return Model{LocalLoRA: rec, Present: true}, nil
}

// Import is Stage and Commit in one call, for a caller that has no step in between (the MCP upload). A file whose name is taken is refused
// and nothing is kept. adopt takes the base models and trigger words the file itself names into the record when in.Meta does not give them
// (never the strength or the license: only what a user or an agent states is recorded for those).
func (s *Service) Import(ctx context.Context, originalName string, declared int64, r io.Reader, in Input, adopt bool) (Model, error) {
	st, err := s.Stage(ctx, originalName, declared, r)
	if err != nil {
		return Model{}, err
	}
	in.Token = st.Token
	if adopt {
		if len(in.BaseModels) == 0 {
			in.BaseModels = st.Meta.BaseModels
		}
		if strings.TrimSpace(in.TriggerWords) == "" {
			in.TriggerWords = strings.Join(st.Meta.TriggerWords, ", ")
		}
	}
	m, err := s.Commit(ctx, in)
	if err != nil {
		s.Discard(st.Token)
	}
	return m, err
}
