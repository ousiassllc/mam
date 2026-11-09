// Package lorafiletest builds .safetensors files for the tests of the LoRA import (screen, service, MCP): well-formed ones (a header whose tensors
// cover the data exactly) and broken ones. Not for production code.
package lorafiletest

import (
	"encoding/binary"
	"encoding/json"
	"sort"
)

// LoRATensors are tensor names of a LoRA (the kohya layout).
var LoRATensors = []string{"lora_unet_blocks_0.lora_down.weight", "lora_unet_blocks_0.lora_up.weight", "lora_unet_blocks_0.alpha"}

// File is a well-formed .safetensors file of the tensors (each an F32 vector of 2) with the __metadata__ (nil: none).
func File(tensors []string, meta map[string]string) []byte { return Padded(tensors, meta, 0) }

// Padded is File with extra data bytes of zeros after the tensors, so that the file has a size a test can aim at (the data is then longer than
// the tensors cover: not well-formed, unless extra is 0).
func Padded(tensors []string, meta map[string]string, extra int) []byte {
	names := append([]string(nil), tensors...)
	sort.Strings(names)
	hdr := map[string]any{}
	off := 0
	for _, n := range names {
		hdr[n] = map[string]any{"dtype": "F32", "shape": []int{2}, "data_offsets": []int{off, off + 8}}
		off += 8
	}
	if meta != nil {
		hdr["__metadata__"] = meta
	}
	return assemble(hdr, off+extra)
}

// LoRA is a well-formed LoRA file with the metadata.
func LoRA(meta map[string]string) []byte { return File(LoRATensors, meta) }

// Checkpoint is a well-formed file with the tensor names of an all-in-one SDXL checkpoint (not a LoRA).
func Checkpoint() []byte {
	return File([]string{"model.diffusion_model.a", "conditioner.embedders.1.model.b", "first_stage_model.c"}, nil)
}

// Truncated is a LoRA file with its last bytes cut off.
func Truncated() []byte {
	b := LoRA(nil)
	return b[:len(b)-4]
}

// HugeHeaderLength is a file that starts with a header length no header can have.
func HugeHeaderLength() []byte {
	b := make([]byte, 64)
	binary.LittleEndian.PutUint64(b, 1<<40)
	return b
}

// Garbage is bytes that are not a .safetensors file at all.
func Garbage() []byte {
	return []byte("this is not a safetensors file, only text; it is long enough to hold a length")
}

func assemble(hdr map[string]any, dataLen int) []byte {
	raw, _ := json.Marshal(hdr)
	out := make([]byte, 8, 8+len(raw)+dataLen)
	binary.LittleEndian.PutUint64(out, uint64(len(raw)))
	out = append(out, raw...)
	return append(out, make([]byte, dataLen)...)
}
