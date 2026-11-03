// Package voicetraintest tests voicetrain.Service over fakes: the dataset of a voice, the refusals before a training starts, the run and the voice that comes out.
// It is a package of its own because the directory of the service is at its line budget (linterly); the test of the internals (progress) stays in voicetrain.
package voicetraintest_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/voicetrain"
)

// TestMain turns the test binary into the fake driver of the fine-tuning when MAM_FAKE_TRAIN is set: the training starts os.Args[0] again with the step
// and the job file, which then writes what the real driver and the vendor's scripts write (the mode says how it behaves).
func TestMain(m *testing.M) {
	if mode := os.Getenv("MAM_FAKE_TRAIN"); mode != "" {
		os.Exit(fakeDriver(mode, os.Args[1], os.Args[2]))
	}
	os.Exit(m.Run())
}

type fakeJob struct {
	Work     string `json:"work_dir"`
	Exp      string `json:"exp_dir"`
	Out      string `json:"out_dir"`
	EpochsS2 int    `json:"epochs_sovits"`
	EpochsS1 int    `json:"epochs_gpt"`
	BatchS2  int    `json:"batch_sovits"`
	Clips    int    `json:"clips"`
	List     string `json:"list_path"`
	Wav      string `json:"wav_dir"`
}

func fakeDriver(mode, step, jobPath string) int {
	raw, err := os.ReadFile(jobPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 3
	}
	var j fakeJob
	_ = json.Unmarshal(raw, &j)
	progress := func(step string, done, total int) {
		fmt.Printf("MAM_TRAIN {\"step\": %q, \"done\": %d, \"total\": %d}\n", step, done, total)
	}
	_ = os.MkdirAll(j.Exp, 0o700)
	_ = os.MkdirAll(j.Out, 0o700)
	if mode == "fail-"+step {
		fmt.Fprintln(os.Stderr, "RuntimeError: CUDA out of memory. Tried to allocate 2.00 GiB")
		return 1
	}
	if mode == "disk-"+step {
		fmt.Fprintln(os.Stderr, "OSError: [Errno 28] No space left on device")
		return 1
	}
	switch step {
	case "text", "semantic":
	case "hubert":
		progress("hubert", j.Clips, j.Clips)
	case "sovits", "gpt":
		epochs := j.EpochsS2
		if step == "gpt" {
			epochs = j.EpochsS1
		}
		for n := 1; n <= epochs; n++ {
			time.Sleep(80 * time.Millisecond)
			progress(step, n, epochs)
			if mode == "hang-"+step && n == 1 {
				_ = os.WriteFile(filepath.Join(j.Work, "hung.pid"), []byte(strconv.Itoa(os.Getpid())), 0o600)
				select {} // until the group is killed
			}
		}
		name := map[string]string{"sovits": "sovits.pth", "gpt": "gpt.ckpt"}[step]
		_ = os.WriteFile(filepath.Join(j.Out, name), []byte("weights of "+step), 0o600)
	}
	return 0
}

// fakeHeld is the local runtime of GPT-SoVITS reserved for a training; it runs the test binary as the driver.
type fakeHeld struct {
	rt   *fakeRuntime
	work string
}

func (h *fakeHeld) Spec(script string, args, _ []string) (proc.Spec, error) {
	h.rt.specs.Add(1)
	if script != voicetrain.Script {
		return proc.Spec{}, fmt.Errorf("an unexpected script %q", script)
	}
	h.rt.mu.Lock()
	h.rt.steps = append(h.rt.steps, args[0])
	h.rt.mu.Unlock()
	return proc.Spec{Command: os.Args[0], Args: args, Dir: h.work, Env: []string{"MAM_FAKE_TRAIN=" + h.rt.mode}}, nil
}
func (h *fakeHeld) WorkDir() string    { return h.work }
func (h *fakeHeld) GPU() (string, int) { return "NVIDIA GeForce RTX 4070", 12 }
func (h *fakeHeld) End()               { h.rt.ended.Add(1) }

type fakeRuntime struct {
	mu     sync.Mutex
	mode   string
	work   string
	refuse error
	asked  atomic.Int32 // BeginTool calls
	held   atomic.Int32 // the ones that were granted
	ended  atomic.Int32
	specs  atomic.Int32
	steps  []string
	assets []string
	// gate, when set, holds BeginTool until it is closed; entered says (one value) that BeginTool is waiting there.
	gate    chan struct{}
	entered chan struct{}
}

func (r *fakeRuntime) BeginTool(_ context.Context, id string, assets ...string) (voicetrain.Held, error) {
	r.asked.Add(1)
	r.mu.Lock()
	r.assets = assets
	r.mu.Unlock()
	if r.gate != nil {
		r.entered <- struct{}{}
		<-r.gate
	}
	if id != voicetrain.RuntimeID {
		return nil, fmt.Errorf("an unexpected runtime %q", id)
	}
	if r.refuse != nil {
		return nil, r.refuse
	}
	r.held.Add(1)
	return &fakeHeld{rt: r, work: r.work}, nil
}

type fakeProbe map[string]float64

func (p fakeProbe) AudioDuration(_ context.Context, path string) (float64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	// The test wav says its length in its first sample bytes after the header: see wavOf.
	if len(b) < 48 {
		return 0, fmt.Errorf("not audio")
	}
	return float64(binary.LittleEndian.Uint16(b[44:46])), nil
}

type guard struct{ err error }

func (g guard) Before(context.Context) error { return g.err }
