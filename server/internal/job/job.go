// Package job 是定时任务的**代码注册表**。
//
// 与 internal/perm 是同一个模式：任务的真源在代码里，数据库只存「开关 + cron 表达式」。
//
// 若依的做法是「库里的 cron + 反射调用 bean 方法」，Go 里走不通——
// 没有安全的反射调用任意函数的方式。所以只能代码注册，
// 界面上也**不提供新建任务**：造出来的任务永远不会被执行。
package job

import (
	"context"
	"fmt"
	"sort"
)

// Job 是一个可调度的任务。
type Job struct {
	// Key 是稳定标识，落库并用于接口路径。改名等同于「删旧建新」。
	Key string
	// DescriptionKey 是说明文案的 i18n 键，前端渲染。
	DescriptionKey string
	// DefaultCron 仅在数据库里还没有这个任务时使用；
	// 已有记录（用户改过表达式）不会被覆盖。
	DefaultCron string
	// Run 是任务本体。ctx 带超时，任务应当尊重它。
	Run func(ctx context.Context) error
}

// Registry 持有全部已注册的任务。
type Registry struct {
	byKey map[string]Job
	order []string
}

func NewRegistry() *Registry {
	return &Registry{byKey: map[string]Job{}}
}

// Register 注册一个任务。
//
// key 重复直接 panic：那是编程错误，应当在启动时就崩，
// 而不是让两个任务互相覆盖、表现为「某个任务时好时坏」。
func (r *Registry) Register(j Job) {
	if j.Key == "" {
		panic("任务缺少 key")
	}
	if j.Run == nil {
		panic(fmt.Sprintf("任务 %s 没有实现 Run", j.Key))
	}
	if j.DefaultCron == "" {
		panic(fmt.Sprintf("任务 %s 没有默认 cron", j.Key))
	}
	if _, dup := r.byKey[j.Key]; dup {
		panic(fmt.Sprintf("任务 key %q 重复注册", j.Key))
	}
	r.byKey[j.Key] = j
	r.order = append(r.order, j.Key)
}

// Get 按 key 取任务。
func (r *Registry) Get(key string) (Job, bool) {
	j, ok := r.byKey[key]
	return j, ok
}

// All 按注册顺序返回全部任务，顺序稳定（便于界面展示与测试断言）。
func (r *Registry) All() []Job {
	out := make([]Job, 0, len(r.order))
	for _, key := range r.order {
		out = append(out, r.byKey[key])
	}
	return out
}

// Keys 返回全部已注册的 key，已排序。
func (r *Registry) Keys() []string {
	out := make([]string, 0, len(r.byKey))
	for key := range r.byKey {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// Unknown 返回 codes 中未注册的 key。
//
// 注意这里**不用于拒绝启动**，与 perm.Unknown 的处理相反：库里多出一条
// 未知任务只是「一条永远不会跑的记录」，而权限码多一条会让授权静默失效——
// 后者的后果严重得多。所以任务这里只告警。
func (r *Registry) Unknown(keys []string) []string {
	var out []string
	for _, k := range keys {
		if _, ok := r.byKey[k]; !ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
