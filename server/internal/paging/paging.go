// 有问题欢迎联系 hot_kun@hotmail.com
// Package paging 提供所有列表接口共用的分页参数规范化。
//
// 独立成包而不是留在 service 里：会话列表在 auth 包，而 service 已经导入了 auth，
// 两边都要用同一套规则时，规则只能待在两者都能依赖的更底层。
// 复制一份的实现迟早会分叉，而分页这种东西一旦分叉，表现只是"某一页少了几条"。
package paging

// Default 与 Max 是 page_size 的默认值与上限。
const (
	Default = 20
	Max     = 100
)

// Normalize 把请求里的 page / pageSize 修正到合法范围。
//
// 超出范围时回落而不是报错：分页参数是展示细节，
// 为此拒绝整个请求对用户没有好处。
func Normalize(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = Default
		return page, pageSize
	}
	if pageSize > Max {
		// 钳到上限，而不是悄悄换成默认值。
		// 要 101 条却拿到 20 条，调用方会以为数据只有这么多。
		pageSize = Max
	}
	return page, pageSize
}

// Clamp 把越界的页码钳到最后一页，返回（实际页码, 偏移量）。
//
// 不钳的话 `page=99`（共 2 页）会返回空列表，但响应里的 page 仍是 99——
// 前端分页器就显示 "99 / 2" 这种不可能的状态，点「上一页」还是空白。
// 钳到最后一页后，一个过期的书签至少还能看到数据。
func Clamp(page, pageSize int, total int64) (int, int) {
	if total <= 0 {
		return 1, 0
	}
	last := int((total + int64(pageSize) - 1) / int64(pageSize))
	if page > last {
		page = last
	}
	return page, (page - 1) * pageSize
}
