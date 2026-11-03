package irodori

import (
	"encoding/binary"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
)

// MaxReferenceDuration is the combined length of reference recordings Irodori-TTS takes (the model cards: "up to 120 seconds of combined
// reference audio"); what is longer is cut by the server, so mam refuses it before it sends anything.
const MaxReferenceDuration = 120 * time.Second

// CheckReferences refuses reference voices Irodori-TTS cannot be given, before anything is sent to its server: a voice without a usable
// recording, an empty file, a file that is not in a format the server reads (the signature of the content must match the extension: the
// server decodes what it is given and answers a 500 with the decoder's own words), and recordings that together are longer than
// MaxReferenceDuration. The length is the one measured when the recording was set (localvoice.Voice.Duration), or the header of a wav; a
// recording whose length is not known is let through (the server cuts at its own limit). The speaker is the one of the line.
func CheckReferences(speaker string, voices ...localvoice.Voice) error {
	refuse := func(problem string) error {
		return &localvoice.ReferenceError{Speaker: speaker, Problem: problem, Vendor: localvoice.VendorIrodori}
	}
	var total time.Duration
	for _, v := range voices {
		if !v.Usable() {
			return refuse("参照音声がありません")
		}
		d, err := checkFile(v)
		if err != nil {
			return refuse(err.Error())
		}
		total += d
	}
	if total > MaxReferenceDuration {
		return refuse(fmt.Sprintf("参照音声の長さの合計が %d 秒で、Irodori-TTS が受け付ける %d 秒を超えています", int(total.Seconds()), int(MaxReferenceDuration.Seconds())))
	}
	return nil
}

// CheckAll is CheckReferences for every speaker of a script (the same check the line makes before it is sent, made for all of them at once,
// in the order of the names): the first refusal.
func CheckAll(voices map[string]localvoice.Voice) error {
	for _, name := range sortedNames(voices) {
		if err := CheckReferences(name, voices[name]); err != nil {
			return err
		}
	}
	return nil
}

// checkFile checks one recording (empty, format) and returns its length (0 when it is not known).
func checkFile(v localvoice.Voice) (time.Duration, error) {
	f, err := os.Open(v.Path) //nolint:gosec // the path of the Character's reference voice, below the assets directory
	if err != nil {
		return 0, fmt.Errorf("参照音声のファイルを開けません（%s）", filepath.Base(v.Path))
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, 64)
	n, _ := io.ReadFull(f, head) // a file shorter than the buffer is read as far as it goes
	head = head[:n]
	if n == 0 {
		return 0, fmt.Errorf("参照音声のファイルが空です（%s）", filepath.Base(v.Path))
	}
	format, ok := voiceref.FormatOf(filepath.Ext(v.Path))
	if !ok || !format.Matches(head) {
		return 0, fmt.Errorf("参照音声のファイルの形式が拡張子と合いません（%s。使えるのは %v）", filepath.Base(v.Path), voiceref.Extensions())
	}
	if v.Duration > 0 {
		return v.Duration, nil
	}
	return wavDuration(f, head), nil
}

// wavDuration is the length of a PCM wav from its header (0 for another format, or a header that is not the plain one).
func wavDuration(r io.ReaderAt, head []byte) time.Duration {
	if len(head) < 12 || string(head[:4]) != "RIFF" || string(head[8:12]) != "WAVE" {
		return 0
	}
	var rate, bytesPerSec uint32
	for off := int64(12); ; {
		var chunk [8]byte
		if _, err := r.ReadAt(chunk[:], off); err != nil {
			return 0
		}
		size := int64(binary.LittleEndian.Uint32(chunk[4:]))
		switch string(chunk[:4]) {
		case "fmt ":
			var f [16]byte
			if _, err := r.ReadAt(f[:], off+8); err != nil {
				return 0
			}
			rate, bytesPerSec = binary.LittleEndian.Uint32(f[4:]), binary.LittleEndian.Uint32(f[8:])
		case "data":
			if rate == 0 || bytesPerSec == 0 {
				return 0
			}
			return time.Duration(float64(size) / float64(bytesPerSec) * float64(time.Second))
		}
		off += 8 + size + size%2
	}
}

// sortedNames are the speakers of voices in a fixed order (the first refusal is the same on every run).
func sortedNames(voices map[string]localvoice.Voice) []string {
	return slices.Sorted(maps.Keys(voices))
}
