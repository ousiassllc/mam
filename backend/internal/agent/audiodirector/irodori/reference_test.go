package irodori_test

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/irodori"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
)

// wavHeader is the header of a 16-bit mono wav at 24 kHz that says it holds seconds of audio. Only the header is written: the checks read
// the header, not the samples.
func wavHeader(seconds int) []byte {
	const rate, bytesPerSec = 24000, 48000
	h := make([]byte, 0, 44)
	h = append(h, "RIFF"...)
	h = binary.LittleEndian.AppendUint32(h, uint32(36+seconds*bytesPerSec))
	h = append(h, "WAVEfmt "...)
	h = binary.LittleEndian.AppendUint32(h, 16)
	h = binary.LittleEndian.AppendUint16(h, 1)
	h = binary.LittleEndian.AppendUint16(h, 1)
	h = binary.LittleEndian.AppendUint32(h, rate)
	h = binary.LittleEndian.AppendUint32(h, bytesPerSec)
	h = binary.LittleEndian.AppendUint16(h, 2)
	h = binary.LittleEndian.AppendUint16(h, 16)
	h = append(h, "data"...)
	return binary.LittleEndian.AppendUint32(h, uint32(seconds*bytesPerSec))
}

func writeFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(p, data, 0o600))
	return p
}

func refusal(t *testing.T, err error) *localvoice.ReferenceError {
	t.Helper()
	var re *localvoice.ReferenceError
	require.True(t, errors.As(err, &re), "got %v", err)
	assert.Equal(t, localvoice.RoleVoiceIrodori, re.Role())
	return re
}

func TestCheckReferences_AcceptsAWavOfTenSeconds(t *testing.T) {
	v := localvoice.Voice{Path: writeFile(t, "yui.wav", wavHeader(10))}

	assert.NoError(t, irodori.CheckReferences("ユイ", v))
}

func TestCheckReferences_RefusesWhatIrodoriCannotBeGiven(t *testing.T) {
	for name, tc := range map[string]struct {
		voice localvoice.Voice
		want  string
	}{
		"no recording":              {localvoice.Voice{}, "参照音声がありません"},
		"a recording that is gone":  {localvoice.Voice{Path: filepath.Join(t.TempDir(), "gone.wav")}, "開けません"},
		"an empty file":             {localvoice.Voice{Path: writeFile(t, "empty.wav", nil)}, "空です"},
		"mp3 content named wav":     {localvoice.Voice{Path: writeFile(t, "x.wav", []byte("ID3\x04\x00\x00\x00\x00\x00\x00"))}, "拡張子と合いません"},
		"an extension no voice has": {localvoice.Voice{Path: writeFile(t, "x.txt", wavHeader(5))}, "拡張子と合いません"},
		"a wav longer than 120 s":   {localvoice.Voice{Path: writeFile(t, "long.wav", wavHeader(121))}, "120 秒"},
	} {
		t.Run(name, func(t *testing.T) {
			re := refusal(t, irodori.CheckReferences("ユイ", tc.voice))

			assert.Equal(t, "ユイ", re.Speaker)
			assert.Contains(t, re.Problem, tc.want)
		})
	}
}

func TestCheckReferences_TheLengthIsTheMeasuredOneOrTheHeaderAndTheTotalIsChecked(t *testing.T) {
	wav := writeFile(t, "a.wav", wavHeader(5))
	measured := localvoice.Voice{Path: wav, Duration: 90 * time.Second}

	assert.NoError(t, irodori.CheckReferences("ユイ", measured), "the measured length of 90 s is within the limit")
	assert.NoError(t, irodori.CheckReferences("ユイ", localvoice.Voice{Path: wav, Duration: 120 * time.Second}), "120 s is the limit itself")
	assert.Equal(t, "ユイ", refusal(t, irodori.CheckReferences("ユイ", measured, measured)).Speaker, "two recordings of 90 s are 180 s together")
	assert.Equal(t, "ユイ", refusal(t, irodori.CheckReferences("ユイ", localvoice.Voice{Path: wav, Duration: 121 * time.Second})).Speaker)
}

func TestCheckReferences_ARecordingWhoseLengthIsNotKnownPasses(t *testing.T) {
	flac := writeFile(t, "x.flac", []byte("fLaC\x00\x00\x00\x22"))

	assert.NoError(t, irodori.CheckReferences("ユイ", localvoice.Voice{Path: flac}))
}

func TestCheckAll_ReportsTheFirstSpeakerInTheOrderOfTheNames(t *testing.T) {
	voices := map[string]localvoice.Voice{
		"b": {Path: writeFile(t, "b.wav", nil)},
		"a": {Path: writeFile(t, "a.wav", nil)},
		"c": {Path: writeFile(t, "c.wav", wavHeader(5))},
	}

	re := refusal(t, irodori.CheckAll(voices))

	assert.Equal(t, "a", re.Speaker)
	assert.NoError(t, irodori.CheckAll(map[string]localvoice.Voice{"c": voices["c"]}))
	assert.NoError(t, irodori.CheckAll(nil))
}
