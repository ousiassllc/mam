package gpu

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Apple is the Detector of an Apple silicon Mac. The GPU is part of the SoC and has no memory of its own, so the one Adapter it lists
// carries the name of the chip (sysctl machdep.cpu.brand_string: "Apple M2 Pro") and the unified memory of the Mac (hw.memsize) as its
// memory; the GPU can use only a part of it (Metal's recommended working set), so a model that needs nearly all of it may still not fit.
// Commands go through a Runner, so the parsing is tested on any OS with a fake.
type Apple struct{ Runner Runner }

// Adapters implements Detector. An Intel Mac has no Apple GPU the bundled engine can use: it lists none, which is no error.
func (a Apple) Adapters(ctx context.Context) ([]Adapter, error) {
	out, err := a.Runner.Run(ctx, "sysctl", "-n", "machdep.cpu.brand_string")
	if err != nil {
		return nil, fmt.Errorf("sysctl machdep.cpu.brand_string: %w", err)
	}
	brand := strings.TrimSpace(string(out))
	if !strings.HasPrefix(brand, "Apple ") {
		return nil, nil
	}
	out, err = a.Runner.Run(ctx, "sysctl", "-n", "hw.memsize")
	if err != nil {
		return nil, fmt.Errorf("sysctl hw.memsize: %w", err)
	}
	mem, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	if err != nil || mem == 0 {
		return nil, fmt.Errorf("sysctl hw.memsize: unreadable output %q", strings.TrimSpace(string(out)))
	}
	return []Adapter{{Name: brand, VendorID: VendorApple, VRAMBytes: mem}}, nil
}

// MacOSMajor is the major version of macOS (sw_vers -productVersion: "14.6.1" gives 14). The pinned wheels of the macOS engine run on one
// version and later (catalog.AppleEngine.MinMacOS).
func MacOSMajor(ctx context.Context, r Runner) (int, error) {
	out, err := r.Run(ctx, "sw_vers", "-productVersion")
	if err != nil {
		return 0, fmt.Errorf("sw_vers: %w", err)
	}
	major, _, _ := strings.Cut(strings.TrimSpace(string(out)), ".")
	n, err := strconv.Atoi(major)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("sw_vers: unreadable version %q", strings.TrimSpace(string(out)))
	}
	return n, nil
}
