package gpu

import (
	"context"
	"regexp"
	"strconv"
	"strings"
)

// ---- Apple Silicon ----

var (
	appleUtilization = regexp.MustCompile(`"Device Utilization %"\s*=\s*(\d+)`)
	appleInUse       = regexp.MustCompile(`"In use system memory"\s*=\s*(\d+)`)
	vmPageSize       = regexp.MustCompile(`page size of (\d+) bytes`)
)

func (st *Status) readApple(ctx context.Context, o StatusOptions) {
	g := GPU{Name: "Apple GPU", Vendor: "Apple", Memory: "unified", Unavailable: map[string]string{},
		Note: "ユニファイドメモリ: GPU 専用のメモリは無く、システムのメモリを CPU と分け合う。total はメモリ全体、used は GPU が確保している量、free はシステム全体の空き（free + inactive + speculative のページ）。"}
	if out, err := o.Runner.Run(ctx, "sysctl", "-n", "machdep.cpu.brand_string"); err == nil {
		if name := strings.TrimSpace(string(out)); name != "" {
			g.Name = name
		}
	}
	if out, err := o.Runner.Run(ctx, "sysctl", "-n", "hw.memsize"); err != nil {
		g.Unavailable["vram_total_bytes"] = "sysctl hw.memsize を実行できませんでした: " + err.Error()
	} else if v, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64); err != nil || v <= 0 {
		g.Unavailable["vram_total_bytes"] = "sysctl hw.memsize の出力を読めませんでした"
	} else {
		g.VRAMTotalBytes = new(v)
	}
	const ioregWhy = "ioreg の IOAccelerator から読めませんでした"
	if out, err := o.Runner.Run(ctx, "ioreg", "-r", "-d", "1", "-w", "0", "-c", "IOAccelerator"); err != nil {
		why := "ioreg を実行できませんでした: " + err.Error()
		g.Unavailable["vram_used_bytes"], g.Unavailable["utilization_percent"] = why, why
	} else {
		text := string(out)
		if v, ok := firstInt(appleInUse, text); ok {
			g.VRAMUsedBytes = new(v)
		} else {
			g.Unavailable["vram_used_bytes"] = ioregWhy + "（In use system memory がありません）"
		}
		if v, ok := firstInt(appleUtilization, text); ok {
			g.UtilizationPercent = new(int(v))
		} else {
			g.Unavailable["utilization_percent"] = ioregWhy + "（Device Utilization % がありません）"
		}
	}
	if out, err := o.Runner.Run(ctx, "vm_stat"); err != nil {
		g.Unavailable["vram_free_bytes"] = "vm_stat を実行できませんでした: " + err.Error()
	} else if v, ok := vmStatAvailable(string(out)); ok {
		g.VRAMFreeBytes = new(v)
	} else {
		g.Unavailable["vram_free_bytes"] = "vm_stat の出力を読めませんでした"
	}
	g.Unavailable["temperature_celsius"] = "macOS は温度を管理者権限が要る powermetrics でしか出さないため、取得しません"
	st.GPUs = append(st.GPUs, g)
	st.Source = "ioreg"
	st.Unavailable["processes"] = "macOS には GPU を使っているプロセスの一覧を権限なしで読む手段がありません"
}

func firstInt(re *regexp.Regexp, text string) (int64, bool) {
	m := re.FindStringSubmatch(text)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseInt(m[1], 10, 64)
	return v, err == nil
}

// vmStatAvailable is the memory vm_stat counts as available: the free, inactive and speculative pages.
func vmStatAvailable(out string) (int64, bool) {
	size, ok := firstInt(vmPageSize, out)
	if !ok || size <= 0 {
		return 0, false
	}
	var pages int64
	found := false
	for _, line := range strings.Split(out, "\n") {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "Pages free", "Pages inactive", "Pages speculative":
			n, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimSpace(val), "."), 10, 64)
			if err != nil {
				return 0, false
			}
			pages += n
			found = true
		}
	}
	if !found {
		return 0, false
	}
	return pages * size, true
}
