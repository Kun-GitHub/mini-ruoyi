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

// procStatFields 是 /proc/stat 汇总行里参与计算使用率的字段个数。
//
// 那行的布局是：user nice system idle iowait irq softirq steal guest guest_nice
// **只能取前 8 个。** 内核已经把 guest 和 guest_nice 的时间计进了 user 和 nice，
// 全部加起来等于把这部分算两遍，使用率会偏高。
const procStatFields = 8

// readProcStat 读 /proc/stat 的第一行（汇总行）：
// cpu user nice system idle iowait irq softirq steal ...
func readProcStat() (procStat, bool) {
	body, err := os.ReadFile("/proc/stat")
	if err != nil {
		return procStat{}, false
	}
	line, _, _ := strings.Cut(string(body), "\n")
	return parseProcStat(line)
}

func parseProcStat(line string) (procStat, bool) {
	fields := strings.Fields(line)
	// cpu、user、nice、system、idle 是最少的五个字段
	if len(fields) < 5 || fields[0] != "cpu" {
		return procStat{}, false
	}

	values := fields[1:]
	if len(values) > procStatFields {
		values = values[:procStatFields]
	}

	var out procStat
	for i, f := range values {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			return procStat{}, false
		}
		out.total += v
		// 下标 3 是 idle、4 是 iowait。iowait 期间 CPU 并没在干活，算空闲
		if i == 3 || i == 4 {
			out.idle += v
		}
	}
	return out, true
}

// readLoadAvg 读 /proc/loadavg。
func readLoadAvg() ([]float64, bool) {
	body, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return nil, false
	}
	return parseLoadAvg(string(body))
}

func parseLoadAvg(body string) ([]float64, bool) {
	fields := strings.Fields(body)
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
	return parseMeminfo(string(body))
}

func parseMeminfo(body string) Memory {
	values := map[string]int64{}
	for _, line := range strings.Split(string(body), "\n") {
		key, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		// /proc/meminfo 的值一律带 kB 单位（连 HugePages_* 也是）。
		// 不认这个单位就不认这行：宁可不报，也不能把 "1000 MB" 当成 1000 kB 报出去。
		if len(fields) != 2 || fields[1] != "kB" {
			continue
		}
		v, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			continue
		}
		values[key] = v * 1024
	}

	total, hasTotal := values["MemTotal"]
	// 必须区分「键不存在」和「值是 0」：老内核（< 3.14）没有 MemAvailable，
	// 当成 0 会算出「100% 已用」——在监控页面上就是个假告警。
	free, hasFree := values["MemAvailable"]
	if !hasTotal || !hasFree || total <= 0 {
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

// collectDisk 取磁盘占用。具体怎么读交给平台文件（Unix 用 Statfs，
// Windows 用 GetDiskFreeSpaceExW）—— 这部分没法只用一份代码写。
func collectDisk(path string) Disk {
	if path == "" {
		path = "."
	}

	total, avail, free, ok := diskSpace(path)
	if !ok {
		return Disk{Path: path}
	}

	used := total - free
	return Disk{
		Path:      path,
		Available: true,
		Total:     int64(total),
		Used:      int64(used),
		Free:      int64(avail),
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
	return parseStatm(string(body), int64(os.Getpagesize()))
}

// parseStatm 从 /proc/self/statm 取常驻内存。
// 那行是：size resident shared text lib data dt（单位都是页）。
func parseStatm(body string, pageSize int64) (int64, bool) {
	fields := strings.Fields(body)
	if len(fields) < 2 {
		return 0, false
	}
	pages, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return pages * pageSize, true
}

func percent(part, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}
