package supporttest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
)

// An Apple silicon GPU with 64 GiB of unified memory: the Detector of a Mac lists it as one adapter of VendorApple.
func appleGPU() gpu.Adapter {
	return gpu.Adapter{Name: "Apple M4 Max", VendorID: gpu.VendorApple, VRAMBytes: 64 * gib}
}

// #935: a Mac is judged by each runtime on its own. Neither GPT-SoVITS nor MuseTalk is installed on Apple silicon (the pins that
// would assemble them cannot be made from primary sources: local-runtime.md §16), so each says why in its own words, with its own
// cloud provider, and the unified memory of the Mac is never counted as the video memory of an NVIDIA GPU.
func TestEvaluateJudgesAMacPerRuntimeAndNamesTheBlockerOfEach(t *testing.T) {
	rt := func(id string) catalog.Runtime {
		r, ok := catalog.Default().Runtime(id)
		require.True(t, ok, id)
		return r
	}
	for name, p := range map[string]rtm.Platform{
		"Apple silicon": {OS: "darwin", Arch: "arm64"},
		"Intel Mac":     {OS: "darwin", Arch: "amd64"},
	} {
		for _, tc := range []struct {
			id       string
			blockers []string // what the sentence of this runtime (and no other) says
			instead  string
		}{
			{"gpt-sovits", []string{"MPS / CPU", "pyopenjtalk", "jieba_fast"}, "ElevenLabs"},
			{"musetalk", []string{"CUDA 前提", "mmcv 2.0.1", "mlx-community/MuseTalk-1.5-q4", "MIT"}, "Sync Labs"},
		} {
			t.Run(name+" "+tc.id, func(t *testing.T) {
				s := rtm.Evaluate(rt(tc.id), p, report(appleGPU()))
				assert.False(t, s.Supported, "64 GiB of unified memory is not the video memory of an NVIDIA GPU")
				assert.Equal(t, rtm.CodeOS, s.Code)
				assert.Empty(t, s.Variant)
				assert.Empty(t, s.GPU)
				for _, b := range tc.blockers {
					assert.Contains(t, s.Reason, b)
				}
				assert.Contains(t, s.Fallback, tc.instead)
				assert.Contains(t, s.Message(), "macOS では")
			})
		}
	}

	t.Run("the two sentences differ", func(t *testing.T) {
		g := rtm.Evaluate(rt("gpt-sovits"), rtm.Platform{OS: "darwin", Arch: "arm64"}, report(appleGPU()))
		m := rtm.Evaluate(rt("musetalk"), rtm.Platform{OS: "darwin", Arch: "arm64"}, report(appleGPU()))
		assert.NotEqual(t, g.Reason, m.Reason)
		assert.NotContains(t, g.Reason, "mmcv")
		assert.NotContains(t, m.Reason, "pyopenjtalk")
	})
}
