package audiofile

import (
	"encoding/binary"
	"math"
	"time"
)

// wavDuration reads the length of a RIFF/WAVE file from its "fmt " chunk (the bytes per second) and its "data" chunk (the bytes of sound), walking the chunks
// in between ("LIST", "fact"...). A data chunk that says it is longer than the file (a stream written without knowing its end: size 0 or 0xFFFFFFFF, or a
// file that is cut) is taken to run to the end of the file.
func wavDuration(data []byte) (time.Duration, error) {
	var byteRate, dataLen uint64
	var haveData bool
	for pos := 12; pos+8 <= len(data); {
		id, size := string(data[pos:pos+4]), uint64(binary.LittleEndian.Uint32(data[pos+4:]))
		body := pos + 8
		switch id {
		case "fmt ":
			if size < 16 || body+16 > len(data) {
				return 0, unreadable("the fmt chunk is cut")
			}
			byteRate = uint64(binary.LittleEndian.Uint32(data[body+8:]))
			if byteRate == 0 { // sample rate x block align, for a writer that left the average out
				byteRate = uint64(binary.LittleEndian.Uint32(data[body+4:])) * uint64(binary.LittleEndian.Uint16(data[body+12:]))
			}
		case "data":
			dataLen, haveData = min(size, u64(len(data)-body)), true
			if size == 0 || size == 0xFFFFFFFF {
				dataLen = u64(len(data) - body)
			}
		}
		next := uint64(body) + size + size&1 // chunks are padded to an even size
		if next > uint64(len(data)) || next > math.MaxInt {
			break
		}
		pos = int(next)
	}
	if byteRate == 0 || !haveData {
		return 0, unreadable("no fmt or data chunk")
	}
	return lengthOrErr(dataLen, byteRate)
}
