package domain

import (
	"errors"
	"time"
)

// ErrNotFound 表示目标记录不存在。repository 层发现无匹配行时返回它，
// handler 据此映射成 404。各层都只依赖本包，避免 handler 反向依赖 repository。
var ErrNotFound = errors.New("resource not found")

// Device 是示例资源，仅用于验证 CRUD / 分页 / 迁移链路。
// 正式业务实体（用户、角色、菜单、权限）后续加入本包。
type Device struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Location  string    `json:"location"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}
