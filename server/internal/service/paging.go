// 有问题欢迎联系 hot_kun@hotmail.com
package service

import (
	"mini-ruoyi/internal/domain"
	"mini-ruoyi/internal/paging"
)

// Page 是所有列表接口的统一分页结构。
//
// List 用 make 初始化而不是 var，保证空结果序列化成 [] 而不是 null，
// 前端不必为「本页没有数据」和「字段不存在」写两种分支。
type Page[T any] struct {
	List     []T   `json:"list"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

// 分页规则的真源在 internal/paging：会话列表在 auth 包，
// 而 service 已经导入了 auth，两边没法共用 service 里的实现。
// 这里保留同名包装，让本包已有的调用点不必全部改前缀。
func normalizePage(page, pageSize int) (int, int) { return paging.Normalize(page, pageSize) }

func clampPage(page, pageSize int, total int64) (int, int) {
	return paging.Clamp(page, pageSize, total)
}

// orActive 把空状态补成 active。
//
// 默认值属于业务规则，放这一层而不是交给每个调用方记得填：
// 漏填会一路走到数据库的 CHECK 约束，报成一个 500，
// 而真正的原因只是「少了个字段」。
func orActive(status string) string {
	if status == "" {
		return domain.StatusActive
	}
	return status
}
