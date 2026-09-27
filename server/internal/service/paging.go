package service

import "mini-ruoyi/internal/domain"

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

// pageSizeMax 与默认值。超出范围时回落到默认值而不是报错——
// 分页参数是展示细节，为此拒绝整个请求对用户没有好处。
const (
	pageSizeDefault = 20
	pageSizeMax     = 100
)

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = pageSizeDefault
		return page, pageSize
	}
	if pageSize > pageSizeMax {
		// 钳到上限，而不是悄悄换成默认值。
		// 要 101 条却拿到 20 条，调用方会以为数据只有这么多。
		pageSize = pageSizeMax
	}
	return page, pageSize
}

// clampPage 把越界的页码钳到最后一页，返回（实际页码, 偏移量）。
//
// 不钳的话 `page=99`（共 2 页）会返回空列表，但响应里的 page 仍是 99——
// 前端分页器就显示 "99 / 2" 这种不可能的状态，点「上一页」还是空白。
// 钳到最后一页后，一个过期的书签至少还能看到数据。
func clampPage(page, pageSize int, total int64) (int, int) {
	if total <= 0 {
		return 1, 0
	}
	last := int((total + int64(pageSize) - 1) / int64(pageSize))
	if page > last {
		page = last
	}
	return page, (page - 1) * pageSize
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
