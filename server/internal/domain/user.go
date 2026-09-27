package domain

import "time"

// User 对应 sys_users 表。字段顺序与 docs/schema.md 一致。
type User struct {
	ID        int64      `json:"id"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	Status    string     `json:"status"`
	Username  string     `json:"username"`
	Password  string     `json:"-"` // bcrypt 哈希，永远不出现在响应里
	Nickname  string     `json:"nickname"`
	Mobile    string     `json:"mobile"`
	Email     string     `json:"email"`
	LoginIP   string     `json:"login_ip"`
	LoginAt   *time.Time `json:"login_at"` // nil 表示从未登录
}

func (u *User) IsActive() bool { return u.Status == StatusActive }
