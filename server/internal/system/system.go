// Package system 采集运行状态：CPU、内存、磁盘、进程。
//
// 只用标准库：部署目标是 Linux（读 /proc），但开发常在 macOS，
// 所以每个指标都带 available 标记——**拿不到就说拿不到**，
// 而不是返回一个看起来像真数据的 0。
// 0 会被当成「负载很低」，比没有更糟。
package system

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// cpuSampleGap 是计算 CPU 使用率时两次采样的间隔。
//
// /proc/stat 给的是自开机以来的累计值，单次采样只能算出「开机至今的平均值」，
// 对排查当前状况没有意义。必须取两次差值。
const cpuSampleGap = 200 * time.Millisecond

type Snapshot struct {
	OS        string  `json:"os"`
	Arch      string  `json:"arch"`
	GoVersion string  `json:"go_version"`
	UptimeSec int64   `json:"uptime_seconds"`
	CPU       CPU     `json:"cpu"`
	Memory    Memory  `json:"memory"`
	Disk      Disk    `json:"disk"`
	Process   Process `json:"process"`
}

type CPU struct {
	Available     bool      `json:"available"`
	Cores         int       `json:"cores"`
	UsagePct      float64   `json:"usage_percent"`
	LoadAvg       []float64 `json:"load_avg"`
	LoadAvailable bool      `json:"load_available"`
}

type Memory struct {
	// Available 为 false 表示本平台读不到系统内存（比如 macOS）
	Available bool    `json:"available"`
	Total     int64   `json:"total"`
	Used      int64   `json:"used"`
	Free      int64   `json:"free"`
	UsagePct  float64 `json:"usage_percent"`
}

type Disk struct {
	// Path 是采样所在的文件系统挂载点/路径，让数字有上下文
	Path      string  `json:"path"`
	Available bool    `json:"available"`
	Total     int64   `json:"total"`
	Used      int64   `json:"used"`
	Free      int64   `json:"free"`
	UsagePct  float64 `json:"usage_percent"`
}

// Process 是 Go 进程自身的状态。
//
// 1G 内存的机器上，「进程占了多少」比「系统用了多少」更值得盯——
// 它接近 1G 时就会被 OOM Killer 干掉。
type Process struct {
	Goroutines int    `json:"goroutines"`
	HeapAlloc  int64  `json:"heap_alloc"`
	HeapSys    int64  `json:"heap_sys"`
	Sys        int64  `json:"sys"`
	NumGC      uint32 `json:"num_gc"`
	// RSSAvailable 为 false 时看 HeapSys/Sys 就够
	RSSAvailable bool  `json:"rss_available"`
	RSS          int64 `json:"rss"`
}

var startedAt = time.Now()

// Collect 采集一次快照。dataPath 用于选择要观测的磁盘（一般是数据目录）。
func Collect(ctx context.Context, dataPath string) Snapshot {
	s := Snapshot{
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		GoVersion: runtime.Version(),
		UptimeSec: int64(time.Since(startedAt).Seconds()),
		CPU:       collectCPU(ctx),
		Memory:    collectMemory(),
		Disk:      collectDisk(dataPath),
		Process:   collectProcess(),
	}
	return s
}

func collectCPU(ctx context.Context) CPU {
	cpu := CPU{Cores: runtime.NumCPU()}

	load, ok := readLoadAvg()
	if ok {
		cpu.LoadAvg = load
		cpu.LoadAvailable = true
	}

	usage, ok := readCPUUsage(ctx)
	if ok {
		cpu.UsagePct = usage
		cpu.Available = true
	}
	return cpu
}

// readCPUUsage 用两次 /proc/stat 的差值算使用率。
func readCPUUsage(ctx context.Context) (float64, bool) {
	first, ok := readProcStat()
	if !ok {
		return 0, false
	}

	// 两次采样之间要等一下。用 select 而不是裸 Sleep：
	// 客户端断开时不该白白占着这个 goroutine 继续等
	select {
	case <-time.After(cpuSampleGap):
	case <-ctx.Done():
		return 0, false
	}

	second, ok := readProcStat()
	if !ok {
		return 0, false
	}

	totalDelta := second.total - first.total
	if totalDelta <= 0 {
		return 0, false
	}
	idleDelta := second.idle - first.idle
	return float64(totalDelta-idleDelta) / float64(totalDelta) * 100, true
}

type procStat struct{ total, idle uint64 }

// readProcStat 读 /proc/stat 的第一行（汇总行）：
// cpu user nice system idle iowait irq softirq steal ...
func readProcStat() (procStat, bool) {
	body, err := os.ReadFile("/proc/stat")
	if err != nil {
		return procStat{}, false
	}

	line, _, _ := strings.Cut(string(body), "\n")
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return procStat{}, false
	}

	var out procStat
	for i, f := range fields[1:] {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			return procStat{}, false
		}
		out.total += v
		// idle 是第 4 个字段（下标 3），iowait 算作空闲
		if i == 3 || i == 4 {
			out.idle += v
		}
	}
	return out, true
}

// readLoadAvg 读 /proc/loadavg：load1 load5 load15 ...
func readLoadAvg() ([]float64, bool) {
	body, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return nil, false
	}
	fields := strings.Fields(string(body))
	if len(fields) < 3 {
		return nil, false
	}
	out := make([]float64, 0, 3)
	for _, f := range fields[:3] {
		v, err := strconv.ParseFloat(f, 64)
		if err != nil {
			return nil, false
		}
		out = append(out, v)
	}
	return out, true
}

// collectMemory 读 /proc/meminfo。
//
// 用 MemAvailable 而不是 MemFree：MemFree 不含可回收的页缓存，
// 在 Linux 上通常是几百 MB 的差距，照着 MemFree 判断会以为内存快满了。
func collectMemory() Memory {
	body, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return Memory{}
	}

	values := map[string]int64{}
	for _, line := range strings.Split(string(body), "\n") {
		key, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		// 单位是 kB
		v, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			continue
		}
		values[key] = v * 1024
	}

	total := values["MemTotal"]
	free := values["MemAvailable"]
	if total <= 0 || free < 0 {
		return Memory{}
	}
	used := total - free

	return Memory{
		Available: true,
		Total:     total,
		Used:      used,
		Free:      free,
		UsagePct:  percent(used, total),
	}
}

// collectDisk 用 Statfs 取磁盘占用。它在各 Unix 上都可用。
func collectDisk(path string) Disk {
	if path == "" {
		path = "/"
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return Disk{Path: path}
	}

	// Bavail 是「非 root 可用」，用它算剩余更贴近实际能写入的空间
	blockSize := uint64(stat.Bsize)
	total := stat.Blocks * blockSize
	free := stat.Bavail * blockSize
	used := total - stat.Bfree*blockSize

	return Disk{
		Path:      path,
		Available: true,
		Total:     int64(total),
		Used:      int64(used),
		Free:      int64(free),
		UsagePct:  percent(int64(used), int64(total)),
	}
}

func collectProcess() Process {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	p := Process{
		Goroutines: runtime.NumGoroutine(),
		HeapAlloc:  int64(ms.HeapAlloc),
		HeapSys:    int64(ms.HeapSys),
		Sys:        int64(ms.Sys),
		NumGC:      ms.NumGC,
	}

	// /proc/self/statm 的第二个字段是常驻内存页数。
	// 用文件而不是 syscall.Getrusage：后者的 Maxrss 在 Linux 上是 KB、
	// 在 macOS 上是字节，同一个字段两种单位，早晚会算错。
	if rss, ok := readRSS(); ok {
		p.RSS = rss
		p.RSSAvailable = true
	}
	return p
}

func readRSS() (int64, bool) {
	body, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(body))
	if len(fields) < 2 {
		return 0, false
	}
	pages, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return pages * int64(os.Getpagesize()), true
}

func percent(part, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}
