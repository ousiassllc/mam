// Package lorafile checks a LoRA file the user picked on their own PC before mam takes it in (Issue #1119; spec model-manager.md
// §14): the name, the format of the .safetensors header, whether the tensors are those of a LoRA, and what the file says of itself
// (the base model, trigger words, ...). Nothing here touches the model folders: the caller copies the file only after Inspect
// accepted it. Every refusal is a *Reject whose message the user can act on.
package lorafile

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hfplan"
)

// MaxBytes is the largest file that is taken in (real LoRAs are 10 MB to 2 GB; a larger file is a checkpoint).
const MaxBytes int64 = 4 << 30

// Reject is a file that cannot be taken in; Message says why, for the user.
type Reject struct{ Message string }

func (e *Reject) Error() string { return e.Message }

func reject(format string, args ...any) error { return &Reject{Message: fmt.Sprintf(format, args...)} }

// Info is what a checked file is: its size and what it says of itself.
type Info struct {
	Size     int64
	Tensors  int
	Metadata Metadata
}

// Inspect reads the file at path and accepts it only when it is a well-formed .safetensors file of a LoRA: an 8-byte header length that
// fits the file (and the header limit), a JSON header whose tensors (dtype, shape, data_offsets) lie inside the data and cover it, and
// tensor names of a LoRA (the same table the Hugging Face import uses to choose the loras folder).
func Inspect(path string) (Info, error) {
	f, err := os.Open(path) // #nosec G304 -- the staged copy that mam wrote
	if err != nil {
		return Info{}, err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return Info{}, err
	}
	return inspect(f, st.Size())
}

func inspect(r io.Reader, size int64) (Info, error) {
	if size <= 0 {
		return Info{}, reject("空のファイルです")
	}
	if size > MaxBytes {
		return Info{}, reject("ファイルが大きすぎます（%s。上限 %s）。LoRA ではなくチェックポイントなどの大きなモデルではありませんか", SizeText(size), SizeText(MaxBytes))
	}
	var lenBuf [8]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return Info{}, reject("%s", notSafetensors)
	}
	n := binary.LittleEndian.Uint64(lenBuf[:])
	switch {
	case n == 0 || n > hf.MaxHeaderBytes:
		return Info{}, reject("%s（ヘッダーの長さ %d が範囲外です）", notSafetensors, n)
	case int64(n) > size-8: //nolint:gosec // n is at most MaxHeaderBytes
		return Info{}, reject("%s（ヘッダーの長さ %d がファイルの大きさ %d を超えています）", notSafetensors, n, size)
	}
	raw := make([]byte, n)
	if _, err := io.ReadFull(r, raw); err != nil {
		return Info{}, reject("%s（ヘッダーを読めませんでした）", notSafetensors)
	}
	if raw[0] != '{' {
		return Info{}, reject("%s（ヘッダーが JSON ではありません）", notSafetensors)
	}
	names, meta, err := checkHeader(raw, size-8-int64(n)) //nolint:gosec // n is at most MaxHeaderBytes
	if err != nil {
		return Info{}, err
	}
	if err := checkLoRA(hf.Header{Names: names, Metadata: meta}); err != nil {
		return Info{}, err
	}
	return Info{Size: size, Tensors: len(names), Metadata: ReadMetadata(meta)}, nil
}

const notSafetensors = ".safetensors の形式ではありません（拡張子だけ変えた別のファイルではありませんか）"

type tensorEntry struct {
	DType   string   `json:"dtype"`
	Shape   []int64  `json:"shape"`
	Offsets [2]int64 `json:"data_offsets"`
}

// checkHeader parses the header JSON and checks that its tensors lie inside the data (dataLen bytes) and cover it exactly, as the
// safetensors library requires: a file that fails would not load in ComfyUI either.
func checkHeader(raw []byte, dataLen int64) (names []string, meta map[string]string, err error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, nil, reject("%s（ヘッダーの JSON を読めません）", notSafetensors)
	}
	type span struct{ begin, end int64 }
	var spans []span
	for name, v := range top {
		if name == "__metadata__" {
			meta = stringMap(v)
			continue
		}
		var t tensorEntry
		if json.Unmarshal(v, &t) != nil || t.DType == "" || t.Offsets[0] < 0 || t.Offsets[1] < t.Offsets[0] || t.Offsets[1] > dataLen || slices.ContainsFunc(t.Shape, func(d int64) bool { return d < 0 }) {
			return nil, nil, reject("%s（テンソル %q の記述がファイルの中身と合いません）", notSafetensors, abbreviate(name, 60))
		}
		names = append(names, name)
		spans = append(spans, span{t.Offsets[0], t.Offsets[1]})
	}
	if len(names) == 0 {
		return nil, nil, reject("%s（テンソルがありません）", notSafetensors)
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].begin < spans[j].begin })
	var next int64
	for _, s := range spans {
		if s.begin != next {
			return nil, nil, reject("%s（テンソルのデータの範囲が連続していません。ファイルが途中で切れている可能性があります）", notSafetensors)
		}
		next = s.end
	}
	if next != dataLen {
		return nil, nil, reject("%s（データの大きさがヘッダーの記述と違います。ダウンロードが途中で切れていませんか）", notSafetensors)
	}
	sort.Strings(names)
	return names, meta, nil
}

// stringMap reads a JSON object and keeps its string values (a .safetensors __metadata__ holds nothing else).
func stringMap(v json.RawMessage) map[string]string {
	var m map[string]any
	if json.Unmarshal(v, &m) != nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		if s, ok := val.(string); ok {
			out[k] = s
		}
	}
	return out
}

var folderWords = map[string]string{
	"checkpoints": "チェックポイント（一体型のモデル）", "diffusion_models": "拡散モデル", "text_encoders": "テキストエンコーダー", "vae": "VAE",
}

// checkLoRA accepts the header only when its tensor names are those of a LoRA.
func checkLoRA(h hf.Header) error {
	in := hfplan.Infer(h)
	switch {
	case in.Folder == "loras":
		return nil
	case folderWords[in.Folder] != "":
		return reject("LoRA ではなく、%s のファイルのようです（%s）。LoRA のファイルを選んでください", folderWords[in.Folder], in.Reason)
	}
	return reject("LoRA のファイルとして扱えません（LoRA のテンソル名 lora_unet_・lora_te・lora_A/B・lora_down/up・.alpha が見つかりません）")
}

// SizeText is a size in bytes as the screens say it (1.5 GiB).
func SizeText(n int64) string { return fetch.HumanBytes(uint64(max(n, 0))) } //nolint:gosec // clamped
