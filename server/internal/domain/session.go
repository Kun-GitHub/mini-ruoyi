package domain

import "time"

// Session 是一次登录会话。TokenHash 是 cookie 里那个 token 的 SHA-256，
// 明文 token 只存在于浏览器和响应里，服务端不保存。
type Session struct {
	TokenHash  string    `json:"-"`
	UserID     int64     `json:"-"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	CSRFToken  string    `json:"csrf_token"`
	IP         string    `json:"ip"`
	UserAgent  string    `json:"user_agent"`
}

func (s *Session) Expired(now time.Time) bool { return !now.Before(s.ExpiresAt) }

// SessionView 是会话列表的一行：会话本身 + 用户信息。
//
// 用户名冗余在查询里带上，而不是让前端按 user_id 再查一次：
// 会话列表要的就是「谁、从哪、什么时候」，一次查完最省事。
type SessionView struct {
	Session
	// TokenHash 必须显式给一个 JSON 名：嵌入的 Session.TokenHash 标了 `json:"-"`
	// （避免从 /auth/me 泄露），不覆盖的话会话列表里就没有可用来踢人的标识，
	// 界面上的「强制下线」会永远 404。
	//
	// 暴露它是安全的：它是浏览器里那个明文 token 的 SHA-256，反推不出 token，
	// 也不能拿去认证——服务端只接受明文，比对时再哈希一次。
	TokenHash string `json:"token_hash"`
	// UserID 同理：嵌入的 Session.UserID 也是 `json:"-"`，
	// 而界面要靠它判断「哪条是我自己的会话」（自己不给自己踢）。
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
}
