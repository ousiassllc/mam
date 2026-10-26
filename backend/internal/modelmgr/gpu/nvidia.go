package gpu

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SmiQuery is the nvidia-smi query for the architecture of every NVIDIA GPU: "<name>, <compute capability>".
var smiArgs = []string{"--query-gpu=name,compute_cap", "--format=csv,noheader"}

// ParseSmi reads the output of nvidia-smi --query-gpu=name,compute_cap --format=csv,noheader into the
// compute capability by GPU name. A line it cannot read (an old driver prints "[N/A]") is skipped.
func ParseSmi(out string) map[string]float64 {
	caps := map[string]float64{}
	for _, line := range strings.Split(out, "\n") {
		name, cc, ok := strings.Cut(strings.TrimSpace(line), ",")
		if !ok {
			continue
		}
		if v, err := strconv.ParseFloat(strings.TrimSpace(cc), 64); err == nil && v > 0 {
			caps[strings.TrimSpace(name)] = v
		}
	}
	return caps
}

// RunSmi runs nvidia-smi (the first of paths that starts) and returns the compute capabilities; nil when it
// is not installed or fails. It never shows a console window and gives up after 5 seconds.
func RunSmi(ctx context.Context, paths []string) map[string]float64 {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, p := range paths {
		cmd := exec.CommandContext(ctx, p, smiArgs...) // #nosec G204 -- fixed program names and arguments
		hideWindow(cmd)
		if out, err := cmd.Output(); err == nil {
			return ParseSmi(string(out))
		}
	}
	return nil
}

// capability is the compute capability of the adapter: nvidia-smi's, else the one its name tells.
func capability(a Adapter, caps map[string]float64) (cc float64, known bool) {
	for name, v := range caps {
		if strings.EqualFold(name, a.Name) || strings.Contains(strings.ToLower(a.Name), strings.ToLower(name)) {
			return v, true
		}
	}
	return capabilityByName(a.Name)
}

type generation struct {
	pattern *regexp.Regexp
	cc      float64
}

// generations are the names of NVIDIA GPUs by architecture, newest first (the first match wins). The
// threshold that matters is Turing (7.5), the oldest GPU the CUDA 13 build supports.
var generations = []generation{
	{regexp.MustCompile(`(?i)\b(rtx\s*(pro\s*)?(50\d\d|6000\s*blackwell)|b200|gb200)`), 12.0},
	{regexp.MustCompile(`(?i)\brtx\s*40\d\d|\bl4\d?\b|\brtx\s*\d{4}\s*ada|\bh100|\bh200`), 8.9},
	{regexp.MustCompile(`(?i)\brtx\s*30\d\d|\brtx\s*a\d{3,4}|\ba(100|10|30|40|16)\b`), 8.6},
	{regexp.MustCompile(`(?i)\brtx\s*20\d\d|titan\s*rtx|quadro\s*rtx|\bgtx\s*16\d\d|tesla\s*t4|\bt4\b|\bt\d{3,4}\b`), 7.5},
	{regexp.MustCompile(`(?i)titan\s*v\b|v100`), 7.0},
	{regexp.MustCompile(`(?i)\bgtx\s*10\d\d|titan\s*x(p)?\b|quadro\s*p\d|tesla\s*p\d|\bp[0-9]{3,4}\b`), 6.1},
	{regexp.MustCompile(`(?i)\bgtx\s*[89]\d\d|\bgtx\s*titan|quadro\s*m\d|tesla\s*m\d`), 5.2},
	{regexp.MustCompile(`(?i)\bgt\s*\d{3,4}|\bgtx\s*[2-7]\d\d|\bmx\s*\d{3}|quadro\s*k\d|tesla\s*k\d`), 3.5},
}

func capabilityByName(name string) (float64, bool) {
	for _, g := range generations {
		if g.pattern.MatchString(name) {
			return g.cc, true
		}
	}
	return 0, false
}
