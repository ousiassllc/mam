package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/detect"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
)

// RunDetect handles --detect-gpu and --detect-comfyui, which the installer (and a user) call to look at the
// machine without starting mam: it prints one JSON report and the caller exits. handled is false when neither
// option is among args (the normal start follows). With one option the output is that report; with both it is
// {"gpu": ..., "comfyui": ...}. --detect-output FILE writes the JSON to FILE instead of w (a window-less exe may have
// no standard output to give). Nothing of mam's data directory is touched: no lock, no database.
//
// The reports: gpu.Report (the adapters and the recommended build of the ComfyUI portable package) and
// detect.Report (a ComfyUI answering on 127.0.0.1:8188 or :8000, and the ComfyUI Desktop configuration).
func RunDetect(ctx context.Context, args []string, w io.Writer) (handled bool, err error) {
	wantGPU, wantComfy, outFile := false, false, ""
	for i := 0; i < len(args); i++ {
		name, value, hasValue := strings.Cut(strings.TrimLeft(args[i], "-"), "=")
		if !strings.HasPrefix(args[i], "-") {
			continue
		}
		switch name {
		case "detect-gpu":
			wantGPU = true
		case "detect-comfyui":
			wantComfy = true
		case "detect-output":
			if hasValue {
				outFile = value
			} else if i+1 < len(args) {
				i++
				outFile = args[i]
			}
		}
	}
	if !wantGPU && !wantComfy {
		return false, nil
	}
	var report any
	configDir, _ := os.UserConfigDir()
	switch {
	case wantGPU && wantComfy:
		report = map[string]any{"gpu": gpu.DetectSystem(ctx), "comfyui": detect.Detect(ctx, detect.Options{ConfigDir: configDir})}
	case wantGPU:
		report = gpu.DetectSystem(ctx)
	default:
		report = detect.Detect(ctx, detect.Options{ConfigDir: configDir})
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return true, err
	}
	data = append(data, '\n')
	if outFile != "" {
		if err := fsutil.WriteFileAtomic(outFile, data, 0o644); err != nil { // #nosec G306 -- a report for the installer to read
			return true, fmt.Errorf("write %s: %w", outFile, err)
		}
		return true, nil
	}
	_, err = w.Write(data)
	return true, err
}
