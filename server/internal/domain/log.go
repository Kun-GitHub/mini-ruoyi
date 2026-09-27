package domain

import "time"

// 登录日志的状态取值。
const (
	LoginSuccess = "success"
	LoginFailed  = "failed"
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
