package domain

import "time"

// 登录日志的状态取值。
const (
	LoginSuccess = "success"
	LoginFailed  = "failed"
)

// 任务触发方式的取值。把「定时跑的」和「人点的」分开记，
// 因为「这个任务怎么一天跑了好几遍」的答案通常就在这一列。
const (
	JobTriggerCron   = "cron"
	JobTriggerManual = "manual"
)

// LoginLog 是一次登录尝试。成功与失败都记：只记成功的话，
// 「有人在暴力破解」这件事就完全看不出来。
//
// 没有 updated_at —— 日志被改过就失去审计价值。
type LoginLog struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Username  string    `json:"username"`
	Status    string    `json:"status"`
	// Reason 是失败原因的 i18n 键（如 error.badCredentials），成功时为空
	Reason    string `json:"reason"`
	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
}

// JobLog 是一次定时任务的执行记录。与登录/操作日志同族：只追加、不修改，
// 所以没有 updated_at。
//
// 不存任务描述之类的展示文案——那是代码注册表的职责；
// 只留 job_key，任务从代码里删掉后历史仍然可读（哪怕只剩原始 key）。
type JobLog struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	JobKey    string    `json:"job_key"`
	// Trigger 是触发方式：cron（调度器）或 manual（界面上点了立即执行）
	Trigger string `json:"trigger"`
	Status  string `json:"status"`
	// Error 是失败原因或跳过原因（来自任务的 error），成功时为空
	Error      string `json:"error"`
	DurationMS int    `json:"duration_ms"`
}

// OperLog 是一次写操作的审计记录。
//
// 不记录请求体：登录、改密码这些接口的 body 里是明文密码，
// 而日志表的访问控制比用户表弱得多，把它们写进去等于自己制造一个泄密点。
// method + path 已经能回答「谁在什么时候动了哪个资源」。
type OperLog struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UserID    int64     `json:"user_id"`
	Username  string    `json:"username"`
	Method    string    `json:"method"`
	Path      string    `json:"path"`
	Status    int       `json:"status"`
	// Result 是失败时的 i18n 键，成功时为空
	Result     string `json:"result"`
	DurationMS int    `json:"duration_ms"`
	IP         string `json:"ip"`
	UserAgent  string `json:"user_agent"`
}
