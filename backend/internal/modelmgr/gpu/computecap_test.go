package gpu_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
)

// Detect tells the compute capability of every NVIDIA adapter (a local runtime picks its build by it): nvidia-smi's value, else
// the generation the name tells; another vendor has none.
func TestDetectFillsInTheComputeCapabilityOfNVIDIAAdapters(t *testing.T) {
	r := gpu.Detect(context.Background(), fake{adapters: []gpu.Adapter{
		nv("NVIDIA GeForce RTX 4070", 12*gib),
		nv("NVIDIA GeForce RTX 5090", 32*gib),
		nv("NVIDIA Quadro Mystery", 8*gib),
		{Name: "AMD Radeon RX 7900 XTX", VendorID: gpu.VendorAMD, VRAMBytes: 24 * gib},
	}}, map[string]float64{"NVIDIA GeForce RTX 5090": 12.0}, "windows")
	got := map[string]float64{}
	for _, a := range r.Adapters {
		got[a.Name] = a.ComputeCap
	}
	assert.InDelta(t, 8.9, got["NVIDIA GeForce RTX 4070"], 0, "from the name")
	assert.InDelta(t, 12.0, got["NVIDIA GeForce RTX 5090"], 0, "from nvidia-smi")
	assert.Zero(t, got["NVIDIA Quadro Mystery"], "unknown")
	assert.Zero(t, got["AMD Radeon RX 7900 XTX"])
}
