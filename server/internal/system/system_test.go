package system

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"
)

// 下面的样本都是真实的 /proc 内容。
// 这些解析逻辑只在 Linux 上跑，而开发机通常是 macOS ——
// 没有测试就只能靠「在 Linux 上编译通过」来自我安慰，
// 而那完全不代表解析对了。
const (
	// 8 核机器，含 guest 字段（现代内核都有）。guest 的时间已经算在 user 里了。
	procStatSample = `cpu  10000 200 3000 80000 500 0 100 50 4000 100
cpu0 1250 25 375 10000 62 0 12 6 500 12
cpu1 1250 25 375 10000 62 0 12 6 500 12
intr 12345678 0 0 0
ctxt 87654321
btime 1700000000
processes 12345
procs_running 1
procs_blocked 0
`

	meminfoSample = `MemTotal:        1000000 kB
MemFree:          100000 kB
MemAvailable:     400000 kB
Buffers:           50000 kB
Cached:           200000 kB
SwapTotal:             0 kB
SwapFree:              0 kB
`

	loadavgSample = "0.52 1.25 0.98 2/345 12345\n"

	statmSample = "12345 6789 1234 1 0 890 0\n"
)

func TestParseProcStat(t *testing.T) {
	got, ok := parseProcStat(strings.Split(procStatSample, "\n")[0])
	if !ok {
		t.Fatal("解析失败")
	}

	// user+nice+system+idle+iowait+irq+softirq+steal
	// = 10000+200+3000+80000+500+0+100+50
	wantTotal := uint64(93850)
	// idle + iowait = 80000 + 500
	wantIdle := uint64(80500)

	if got.total != wantTotal {
		t.Errorf("total = %d，期望 %d", got.total, wantTotal)
	}
	if got.idle != wantIdle {
		t.Errorf("idle = %d，期望 %d", got.idle, wantIdle)
	}
}

// guest / guest_nice 已被内核计入 user / nice，
// 一起加起来会把这两个字段算两遍，使用率偏高。
func TestParseProcStatExcludesGuest(t *testing.T) {
	got, _ := parseProcStat(strings.Split(procStatSample, "\n")[0])

	// guest=4000、guest_nice=100。若把它们算进去，total 会是 97950
	if got.total == 93850+4100 {
		t.Fatal("guest/guest_nice 被重复计算了：它们已经包含在 user/nice 里")
	}

	// 用真实样本验证使用率落在合理区间
	usage := float64(got.total-got.idle) / float64(got.total) * 100
	if usage < 0 || usage > 100 {
		t.Errorf("使用率 %.1f 超出 [0,100]", usage)
	}
}

// 老内核（2.6 之前）没有 steal 及之后的字段，只有 5 个也要能算。
func TestParseProcStatMinimalFields(t *testing.T) {
	got, ok := parseProcStat("cpu  1 2 3 4 5")
	if !ok {
		t.Fatal("五个字段的最小行应当能解析")
	}
	if got.total != 15 {
		t.Errorf("total = %d，期望 15", got.total)
	}
	if got.idle != 9 { // idle(4) + iowait(5)
		t.Errorf("idle = %d，期望 9", got.idle)
	}
}

func TestParseProcStatRejectsGarbage(t *testing.T) {
	cases := map[string]string{
		"空串":       "",
		"只有标题":     "cpu",
		"字段不足":     "cpu 1 2 3",
		"首列不是 cpu": "cpu0 1 2 3 4 5",
		"有非数字字段":   "cpu 1 2 abc 4 5",
	}
	for name, line := range cases {
		if _, ok := parseProcStat(line); ok {
			t.Errorf("%s：应当解析失败", name)
		}
	}
}

// 若某些字段缺失，返回 false —— 否则会把「只读到一个 0」当成真实值上报。
func TestParseLoadAvg(t *testing.T) {
	got, ok := parseLoadAvg(loadavgSample)
	if !ok {
		t.Fatal("解析失败")
	}
	if len(got) != 3 {
		t.Fatalf("得到 %d 个值，期望 3", len(got))
	}
	want := []float64{0.52, 1.25, 0.98}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("load[%d] = %v，期望 %v", i, got[i], want[i])
		}
	}

	for _, bad := range []string{"", "1.0 2.0", "a b c"} {
		if _, ok := parseLoadAvg(bad); ok {
			t.Errorf("%q 应当解析失败", bad)
		}
	}
}

func TestParseMeminfo(t *testing.T) {
	got := parseMeminfo(meminfoSample)
	if !got.Available {
		t.Fatal("应当可用")
	}

	const wantTotal = 1000000 * 1024
	// 用 MemAvailable(400000) 而不是 MemFree(100000)：
	// MemFree 不含可回收的页缓存，照它判断会以为内存快满了
	const wantFree = 400000 * 1024

	if got.Total != wantTotal {
		t.Errorf("Total = %d，期望 %d", got.Total, wantTotal)
	}
	if got.Free != wantFree {
		t.Errorf("Free = %d（应当取 MemAvailable），期望 %d", got.Free, wantFree)
	}
	if got.Used != wantTotal-wantFree {
		t.Errorf("Used = %d，期望 %d", got.Used, wantTotal-wantFree)
	}
	if math.Abs(got.UsagePct-60) > 0.01 {
		t.Errorf("UsagePct = %v，期望 60", got.UsagePct)
	}
}

func TestParseMeminfoUnavailable(t *testing.T) {
	cases := map[string]string{
		"空内容": "",
		// macOS 上不会走到这里，但容器里 /proc 可能是空挂载
		"没有 MemTotal": "MemFree: 100 kB\n",
		// 老内核（< 3.14）没有 MemAvailable。当成 0 会报出 100% 已用
		"没有 MemAvail": "MemTotal: 1000 kB\nMemFree: 100 kB\n",
		// 单位不是 kB。忽略单位会差 1000 倍
		"单位是 MB": "MemTotal: 1000 MB\nMemAvailable: 100 MB\n",
		"值不是数字":  "MemTotal: abc kB\nMemAvailable: 100 kB\n",
	}
	for name, body := range cases {
		if got := parseMeminfo(body); got.Available {
			t.Errorf("%s：应当报告不可用，而不是返回 %+v", name, got)
		}
	}
}

func TestParseStatm(t *testing.T) {
	got, ok := parseStatm(statmSample, 4096)
	if !ok {
		t.Fatal("解析失败")
	}
	// 第二个字段是常驻页数
	if want := int64(6789 * 4096); got != want {
		t.Errorf("RSS = %d，期望 %d", got, want)
	}

	if _, ok := parseStatm("12345", 4096); ok {
		t.Error("只有一个字段时应当解析失败")
	}
}

// 在 macOS 上 /proc 不存在，这些必须返回「不可用」而不是 0。
// 返回 0 会被界面当成「负载很低」，比没有更糟。
func TestUnsupportedPlatformsSaySo(t *testing.T) {
	// macOS 上没有 /proc。这时必须报「不可用」而不是返回 0：
	// 界面会把 0 当成「负载很低」，比没有更糟。
	if _, ok := readProcStat(); !ok {
		if m := collectMemory(); m.Available {
			t.Error("没有 /proc/meminfo 却报告内存可用")
		}
		if _, ok := readRSS(); ok {
			t.Error("没有 /proc/self/statm 却读到了 RSS")
		}
	}

	// 无论平台如何，Collect 都不能 panic，且必需字段要有值
	snap := Collect(context.Background(), t.TempDir())
	if snap.Disk.Path == "" {
		t.Error("Disk.Path 不该为空")
	}
	if snap.OS == "" || snap.Arch == "" || snap.GoVersion == "" {
		t.Error("基本信息不该为空")
	}
	if snap.CPU.Cores <= 0 {
		t.Errorf("CPU 核数 = %d，不可能少于 1", snap.CPU.Cores)
	}
	if snap.Process.Goroutines <= 0 {
		t.Errorf("Goroutine 数 = %d，不可能少于 1", snap.Process.Goroutines)
	}
}

// 采集本身要花 200ms 采样 CPU。客户端断开时不该白等。
func TestCollectRespectsContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan Snapshot, 1)
	go func() { done <- Collect(ctx, t.TempDir()) }()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("context 已取消，采集却卡住了")
	}
}

func TestPercent(t *testing.T) {
	if got := percent(50, 100); got != 50 {
		t.Errorf("percent(50,100) = %v", got)
	}
	// 除零要返回 0 而不是 NaN/Inf —— 序列化成 JSON 会直接失败
	if got := percent(1, 0); got != 0 {
		t.Errorf("percent(1,0) = %v，期望 0", got)
	}
}
