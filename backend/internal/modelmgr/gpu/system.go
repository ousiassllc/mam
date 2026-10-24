package gpu

import (
	"context"
	"runtime"
)

// DetectSystem is Detect with the real detector and nvidia-smi of this machine.
func DetectSystem(ctx context.Context) Report {
	d, smi := System()
	return Detect(ctx, d, RunSmi(ctx, smi), runtime.GOOS)
}
