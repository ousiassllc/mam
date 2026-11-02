package gpu

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ---- Windows performance counters (AMD, Intel, and an NVIDIA whose nvidia-smi fails) ----
//
// nvidia-smi is NVIDIA only. Every WDDM 2.x driver (Windows 10 1709 and later, any vendor) feeds the performance counter sets
// "GPU Adapter Memory" (Dedicated Usage: the bytes of dedicated video memory in use) and "GPU Engine" (Utilization
// Percentage per engine and process) that Task Manager shows. They need neither administrator rights nor a vendor tool.
// typeperf is not used: it takes the counter paths in the language of Windows (the sets are called differently on a Japanese
// system), while the WMI classes that expose the same counters (Win32_PerfFormattedData_GPUPerformanceCounters_*) have
// one name everywhere. PowerShell reads them and prints CSV, which the pure functions below parse; the command goes through
// the Runner like every other, and the parsing is tested on every OS with the output of a real machine.
//
// An adapter is found in the instances by its LUID (the locally unique ID of the adapter, which DXGI also reports).

const (
	wddmSource = "windows-counters"
	psExe      = "powershell.exe"
	// psFailFast makes a missing class (a Windows without the counters) a failed command, not an empty answer.
	psFailFast         = "$ErrorActionPreference = 'Stop'; "
	psAdapterMemoryCmd = psFailFast + "Get-CimInstance -ClassName Win32_PerfFormattedData_GPUPerformanceCounters_GPUAdapterMemory | Select-Object Name, DedicatedUsage | ConvertTo-Csv -NoTypeInformation"
	psEngineCmd        = psFailFast + "Get-CimInstance -ClassName Win32_PerfFormattedData_GPUPerformanceCounters_GPUEngine | Select-Object Name, UtilizationPercentage | ConvertTo-Csv -NoTypeInformation"
	wddmNote           = "使用量は Windows のパフォーマンスカウンターの値（GPU Adapter Memory の Dedicated Usage、GPU Engine の Utilization Percentage）。使用率は 3D・Compute・Copy などの GPU のエンジンのうち、最も忙しい 1 つの値（タスクマネージャーと同じ見方）。専用メモリを持たない統合 GPU は共有メモリを使うので、used が total を超えることがある。"
	wddmNoTemperature  = "NVIDIA 以外の GPU の温度は取得できません（Windows の標準のカウンターに温度は無く、nvidia-smi は NVIDIA 専用です）"
)

func psArgs(script string) []string {
	return []string{"-NoProfile", "-NonInteractive", "-Command", script}
}

// wddmGPU is a GPU of Status.GPUs that the counters are asked about.
type wddmGPU struct {
	index int
	luid  uint64
}

// readWDDM fills the memory in use and the utilization of the listed GPUs from the counters. Each of the two sets is read
// by its own command and fails on its own; whatever is not read keeps (or gets) its reason in Unavailable.
func (st *Status) readWDDM(ctx context.Context, o StatusOptions, gpus []wddmGPU) {
	var mem map[uint64]int64
	memOut, err := o.Runner.Run(ctx, psExe, psArgs(psAdapterMemoryCmd)...)
	if err == nil {
		mem, err = parseWDDMMemory(string(memOut))
	}
	memWhy := "" // why no GPU has its memory
	if err != nil {
		memWhy = wddmReason("GPU Adapter Memory", err)
	}
	var util map[uint64]int
	utilOut, err := o.Runner.Run(ctx, psExe, psArgs(psEngineCmd)...)
	if err == nil {
		util, err = parseWDDMEngines(string(utilOut))
	}
	utilWhy := ""
	if err != nil {
		utilWhy = wddmReason("GPU Engine", err)
	}
	for _, w := range gpus {
		g := &st.GPUs[w.index]
		g.applyWDDMMemory(mem, w.luid, memWhy)
		g.applyWDDMUtilization(util, w.luid, utilWhy)
		if g.Vendor != "NVIDIA" {
			g.Unavailable["temperature_celsius"] = wddmNoTemperature
		}
		if g.VRAMUsedBytes != nil || g.UtilizationPercent != nil {
			g.Note = wddmNote
			if st.Source == "adapters" {
				st.Source = wddmSource
			}
		}
	}
}

func wddmReason(set string, err error) string {
	if errors.Is(err, errNoInstances) {
		return "Windows のパフォーマンスカウンター（" + set + "）に値がありません（WDDM 2.0 以降のドライバーが要ります）"
	}
	return "Windows のパフォーマンスカウンター（" + set + "）を PowerShell の Get-CimInstance で読めませんでした: " + withStderr(err)
}

// maxStderrBytes is how much of a failed command's standard error goes into a reason (PowerShell's messages are long).
const maxStderrBytes = 300

// withStderr is err's text, followed by the start of what the failed command printed to its standard error when err is
// an exit error that has it ("exit status 1" alone does not say that the class does not exist).
func withStderr(err error) string {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return err.Error()
	}
	msg := strings.Join(strings.Fields(strings.ToValidUTF8(string(exit.Stderr), "")), " ")
	if msg == "" {
		return err.Error()
	}
	if len(msg) > maxStderrBytes {
		cut := maxStderrBytes
		for !utf8.RuneStart(msg[cut]) {
			cut--
		}
		msg = msg[:cut] + "…"
	}
	return err.Error() + ": " + msg
}

func (g *GPU) applyWDDMMemory(mem map[uint64]int64, luid uint64, why string) {
	used, ok := mem[luid]
	if why == "" && !ok {
		why = "このアダプターの GPU Adapter Memory のカウンターがありません"
	}
	if why != "" {
		g.Unavailable["vram_used_bytes"], g.Unavailable["vram_free_bytes"] = why, why
		return
	}
	delete(g.Unavailable, "vram_used_bytes")
	g.VRAMUsedBytes = new(used)
	switch {
	case g.VRAMTotalBytes == nil:
		g.Unavailable["vram_free_bytes"] = "専用メモリの大きさが分からないため、空きを計算できません"
	case used > *g.VRAMTotalBytes:
		g.Unavailable["vram_free_bytes"] = "使用量が専用メモリの大きさを超えています（共有メモリを使っています）"
	default:
		delete(g.Unavailable, "vram_free_bytes")
		g.VRAMFreeBytes = new(*g.VRAMTotalBytes - used)
	}
}

func (g *GPU) applyWDDMUtilization(util map[uint64]int, luid uint64, why string) {
	pct, ok := util[luid]
	if why == "" && !ok {
		why = "このアダプターの GPU Engine のカウンターがありません"
	}
	if why != "" {
		g.Unavailable["utilization_percent"] = why
		return
	}
	delete(g.Unavailable, "utilization_percent")
	g.UtilizationPercent = new(pct)
}

// errNoInstances says the counter set answered with no instance at all.
var errNoInstances = errors.New("no instance")

var (
	// luid_0x00000000_0x0000d4b3_phys_0
	wddmMemoryInstance = regexp.MustCompile(`(?i)^luid_0x([0-9a-f]+)_0x([0-9a-f]+)_phys_\d+$`)
	// pid_14216_luid_0x00000000_0x0000d4b3_phys_0_eng_0_engtype_3D
	wddmEngineInstance = regexp.MustCompile(`(?i)^pid_\d+_luid_0x([0-9a-f]+)_0x([0-9a-f]+)_(phys_\d+_eng_\d+_engtype_.+)$`)
)

// luidOf joins the two halves of a LUID as the instance name prints them (high, low), the way DXGI's adapter LUID reads.
func luidOf(high, low string) (uint64, bool) {
	h, err1 := strconv.ParseUint(high, 16, 32)
	l, err2 := strconv.ParseUint(low, 16, 32)
	return h<<32 | l, err1 == nil && err2 == nil
}

// parseWDDMMemory reads the CSV of Name, DedicatedUsage (ConvertTo-Csv of the GPUAdapterMemory instances): the dedicated
// memory in use in bytes, by adapter LUID (the instances of one adapter, one per physical GPU node, are added up).
func parseWDDMMemory(out string) (map[uint64]int64, error) {
	rows, err := csvRows(out, "Name", "DedicatedUsage")
	if err != nil {
		return nil, err
	}
	used := map[uint64]int64{}
	for _, r := range rows {
		m := wddmMemoryInstance.FindStringSubmatch(r[0])
		v, ok := counterValue(r[1])
		if m == nil || !ok {
			continue // _Total and the like, or a value that is not a number
		}
		if luid, ok := luidOf(m[1], m[2]); ok && v <= math.MaxInt64 {
			used[luid] += int64(v)
		}
	}
	return used, nil
}

// parseWDDMEngines reads the CSV of Name, UtilizationPercentage (the GPUEngine instances, one per process and engine). The
// processes that use an engine are added up; the utilization of an adapter is that of its busiest engine, at most 100.
func parseWDDMEngines(out string) (map[uint64]int, error) {
	rows, err := csvRows(out, "Name", "UtilizationPercentage")
	if err != nil {
		return nil, err
	}
	type engine struct {
		luid uint64
		node string // phys_0_eng_0_engtype_3D
	}
	sums := map[engine]float64{}
	for _, r := range rows {
		m := wddmEngineInstance.FindStringSubmatch(r[0])
		v, ok := counterValue(r[1])
		if m == nil || !ok {
			continue
		}
		if luid, ok := luidOf(m[1], m[2]); ok {
			sums[engine{luid, strings.ToLower(m[3])}] += v
		}
	}
	busiest := map[uint64]int{} // an adapter that has instances and no work is there with its real 0
	for e, v := range sums {
		busiest[e.luid] = max(busiest[e.luid], int(math.Round(min(v, 100))))
	}
	return busiest, nil
}

// counterValue reads a counter cell: a non-negative number.
func counterValue(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v, err == nil && v >= 0 && !math.IsInf(v, 0)
}

// csvRows reads the CSV that ConvertTo-Csv -NoTypeInformation prints (a header and quoted rows; a BOM or a CR may come
// along) and returns the cells of the wanted columns, in the order asked. errNoInstances when there is no row.
func csvRows(out string, cols ...string) ([][]string, error) {
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(strings.TrimSpace(out), "\ufeff")))
	r.FieldsPerRecord = -1
	recs, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("CSV を読めません: %w", err)
	}
	if len(recs) == 0 {
		return nil, errNoInstances
	}
	pos := make([]int, len(cols))
	for i, c := range cols {
		pos[i] = -1
		for j, h := range recs[0] {
			if strings.EqualFold(strings.TrimSpace(h), c) {
				pos[i] = j
			}
		}
		if pos[i] < 0 {
			return nil, fmt.Errorf("CSV に列 %s がありません", c)
		}
	}
	var rows [][]string
	for _, rec := range recs[1:] {
		row := make([]string, len(cols))
		short := false
		for i, p := range pos {
			if p >= len(rec) {
				short = true
				break
			}
			row[i] = rec[p]
		}
		if !short {
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 {
		return nil, errNoInstances
	}
	return rows, nil
}
