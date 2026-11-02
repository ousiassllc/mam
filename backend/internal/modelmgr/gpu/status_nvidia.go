package gpu

import (
	"context"
	"errors"
	"math"
	"os/exec"
	"strconv"
	"strings"
)

// ---- NVIDIA ----

const (
	smiGPUQuery = "--query-gpu=index,uuid,name,memory.total,memory.used,memory.free,utilization.gpu,temperature.gpu"
	smiAppQuery = "--query-compute-apps=gpu_uuid,pid,process_name,used_memory"
	smiFormat   = "--format=csv,noheader,nounits"
	mib         = int64(1) << 20
)

func (st *Status) readNvidia(ctx context.Context, o StatusOptions) {
	paths := o.SmiPaths
	if len(paths) == 0 {
		paths = []string{"nvidia-smi"}
	}
	var out []byte
	var smiErr error
	var smi string
	for _, p := range paths {
		out, smiErr = o.Runner.Run(ctx, p, smiGPUQuery, smiFormat)
		if smiErr == nil {
			smi = p
			break
		}
	}
	reason := ""
	if smiErr != nil {
		reason = smiReason(smiErr)
	}
	var uuids map[string]int
	if smiErr == nil {
		var gpus []GPU
		gpus, uuids = parseSmiGPUs(string(out))
		st.GPUs = append(st.GPUs, gpus...)
		if len(gpus) > 0 {
			st.Source = "nvidia-smi"
		}
	}
	st.addAdapters(ctx, o, smiErr == nil, reason)
	switch {
	case smiErr != nil:
		st.Unavailable["processes"] = reason
	case len(uuids) > 0:
		st.readSmiProcesses(ctx, o, smi, uuids)
	}
	if len(st.GPUs) == 0 {
		why := "GPU が見つかりません"
		if reason != "" {
			why += "（" + reason + "）"
		}
		st.Unavailable["gpus"] = why
	}
}

// addAdapters adds the GPUs the adapter list knows and nvidia-smi did not report: all of them when nvidia-smi
// failed, else the ones that are not NVIDIA. They carry the dedicated memory size only.
func (st *Status) addAdapters(ctx context.Context, o StatusOptions, smiWorked bool, smiReason string) {
	if o.Detector == nil {
		return
	}
	adapters, err := o.Detector.Adapters(ctx)
	if err != nil {
		st.Unavailable["adapters"] = "アダプターの一覧を取得できません: " + err.Error()
	}
	for _, a := range adapters {
		if a.Software || (a.VendorID == VendorNVIDIA && smiWorked) {
			continue
		}
		g := GPU{Index: len(st.GPUs), Name: a.Name, Vendor: vendorName(a.VendorID), Memory: "dedicated", Unavailable: map[string]string{}}
		if a.VRAMBytes > 0 && a.VRAMBytes <= math.MaxInt64 {
			g.VRAMTotalBytes = new(int64(a.VRAMBytes))
		} else {
			g.Unavailable["vram_total_bytes"] = "アダプターが専用メモリの大きさを返しませんでした"
		}
		why := "NVIDIA 以外の GPU の使用量・使用率・温度は取得できません（取得に使う nvidia-smi は NVIDIA 専用です）"
		if a.VendorID == VendorNVIDIA {
			why = smiReason
		}
		for _, f := range []string{"vram_used_bytes", "vram_free_bytes", "utilization_percent", "temperature_celsius"} {
			g.Unavailable[f] = why
		}
		st.GPUs = append(st.GPUs, g)
		if st.Source == "none" {
			st.Source = "adapters"
		}
	}
}

func vendorName(id uint32) string {
	switch id {
	case VendorNVIDIA:
		return "NVIDIA"
	case VendorAMD:
		return "AMD"
	case VendorIntel:
		return "Intel"
	}
	return ""
}

// smiReason says why nvidia-smi gave nothing.
func smiReason(err error) string {
	if errors.Is(err, exec.ErrNotFound) {
		return "nvidia-smi が見つかりません（NVIDIA のドライバが入っていないか、PATH にありません）"
	}
	return "nvidia-smi を実行できませんでした: " + err.Error()
}

// parseSmiGPUs reads the output of smiGPUQuery. It also returns the index of each GPU by uuid.
func parseSmiGPUs(out string) ([]GPU, map[string]int) {
	var gpus []GPU
	uuids := map[string]int{}
	for _, line := range strings.Split(out, "\n") {
		f := splitCSV(line)
		if len(f) < 8 {
			continue
		}
		idx, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		g := GPU{Index: idx, Name: strings.Join(f[2:len(f)-5], ", "), Vendor: "NVIDIA", Memory: "dedicated", Unavailable: map[string]string{}}
		v := f[len(f)-5:]
		g.VRAMTotalBytes = smiMiB(&g, "vram_total_bytes", v[0])
		g.VRAMUsedBytes = smiMiB(&g, "vram_used_bytes", v[1])
		g.VRAMFreeBytes = smiMiB(&g, "vram_free_bytes", v[2])
		g.UtilizationPercent = smiInt(&g, "utilization_percent", v[3])
		g.TemperatureCelsius = smiInt(&g, "temperature_celsius", v[4])
		if len(g.Unavailable) == 0 {
			g.Unavailable = nil
		}
		gpus = append(gpus, g)
		uuids[f[1]] = idx
	}
	return gpus, uuids
}

// splitCSV splits a line of nvidia-smi's csv ("a, b, c") and trims every field.
func splitCSV(line string) []string {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	f := strings.Split(line, ",")
	for i := range f {
		f[i] = strings.TrimSpace(f[i])
	}
	return f
}

// smiNumber reads a value of nvidia-smi; ok is false for "[N/A]", "[Not Supported]" and the like.
func smiNumber(s string) (float64, bool) {
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil && v >= 0
}

func smiUnsupported(s string) string {
	return "nvidia-smi が " + strconv.Quote(s) + " を返しました（この GPU・ドライバでは取得できません）"
}

func smiMiB(g *GPU, field, s string) *int64 {
	v, ok := smiNumber(s)
	if !ok {
		g.Unavailable[field] = smiUnsupported(s)
		return nil
	}
	return new(int64(v) * mib)
}

func smiInt(g *GPU, field, s string) *int {
	v, ok := smiNumber(s)
	if !ok {
		g.Unavailable[field] = smiUnsupported(s)
		return nil
	}
	return new(int(v))
}

// readSmiProcesses lists the compute processes. Under Windows' WDDM the driver does not report the memory of a
// process: used_memory is "[N/A]", and the reason says so.
func (st *Status) readSmiProcesses(ctx context.Context, o StatusOptions, smi string, uuids map[string]int) {
	out, err := o.Runner.Run(ctx, smi, smiAppQuery, smiFormat)
	if err != nil {
		st.Unavailable["processes"] = smiReason(err)
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := splitCSV(line)
		if len(f) < 4 {
			continue
		}
		pid, err := strconv.Atoi(f[1])
		if err != nil {
			continue
		}
		name := strings.Join(f[2:len(f)-1], ", ")
		p := Process{PID: pid, Name: name, Owner: o.owner(pid, name)}
		if idx, ok := uuids[f[0]]; ok {
			p.GPUIndex = new(idx)
		}
		if v, ok := smiNumber(f[len(f)-1]); ok {
			p.UsedBytes = new(int64(v) * mib)
		} else {
			p.UsedReason = "プロセスごとの使用量をドライバが返しません（Windows の WDDM ドライバでは nvidia-smi は [N/A] を返します）。ComfyUI の使用量は comfyui[].devices[].comfyui_used_bytes で見てください"
		}
		st.Processes = append(st.Processes, p)
	}
}
