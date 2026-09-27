package domain

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

var (
	// ErrNotFound 目标记录不存在，映射成 404。
	ErrNotFound = errors.New("resource not found")

	// ErrProtected 资源受保护，不允许该操作（如删除内置角色），映射成 403。
	ErrProtected = errors.New("resource is protected")

	// ErrCannotDeleteSelf 不允许删除当前登录用户，映射成 403。
	ErrCannotDeleteSelf = errors.New("cannot delete yourself")

	// ErrLastAdmin 不允许删除最后一个管理员，映射成 409。
	ErrLastAdmin = errors.New("cannot remove the last administrator")

	// ErrInvalidParent 引用的父节点非法（不存在、类型不对、或会形成环），映射成 400。
	ErrInvalidParent = errors.New("invalid parent reference")

	// ErrBadCredentials 用户名或密码错误，映射成 401。
	// 用户名不存在与密码错误共用它，避免把「哪些用户名存在」泄露出去。
	ErrBadCredentials = errors.New("invalid username or password")

	// ErrAccountDisabled 账号已被停用，映射成 403。
	ErrAccountDisabled = errors.New("account is disabled")

	// ErrUnauthorized 未登录或会话已失效，映射成 401。
	ErrUnauthorized = errors.New("not authenticated")

	// ErrForbidden 已登录但缺少所需权限，映射成 403。
	ErrForbidden = errors.New("permission denied")

	// ErrCSRFInvalid CSRF 令牌缺失或不匹配，映射成 403。
	ErrCSRFInvalid = errors.New("invalid csrf token")

	// ErrInvalidPermCode 提交了代码未声明的权限码，映射成 400。
	ErrInvalidPermCode = errors.New("unknown permission code")

	// ErrFileTooLarge 单个文件超过上限，映射成 413。
	ErrFileTooLarge = errors.New("file too large")
	// ErrQuotaExceeded 总容量超过配额，映射成 413。
	ErrQuotaExceeded = errors.New("storage quota exceeded")
	// ErrInvalidFile 文件本身不合法（比如长度为 0），映射成 400。
	ErrInvalidFile = errors.New("invalid file")

	// ErrInvalidJobCron cron 表达式无法解析，映射成 400。
	ErrInvalidJobCron = errors.New("invalid cron expression")

	// ErrCannotKickSelf 不允许踢掉自己当前这条会话，映射成 403。
	// 与「不能删自己」同理：踢自己的效果就是突然被登出，用户会莫名其妙。
	ErrCannotKickSelf = errors.New("cannot kick your own session")

	// ErrDuplicate 唯一字段冲突（用户名、角色编码等），映射成 409。
	// 判定请用 errors.Is，字段名用 errors.As 取 *DuplicateError。
	ErrDuplicate = errors.New("duplicate value")

	// ErrHasDependents 资源存在关联数据，需要调用方确认后再删，映射成 409。
	// 判定请用 errors.Is，影响面数据用 errors.As 取 *DependentsError。
	ErrHasDependents = errors.New("resource has dependents")
)

// DependentsError 携带删除操作的影响面。
//
// 它与 ErrHasDependents 通过 Is 关联，所以调用方可以：
//
//	if errors.Is(err, domain.ErrHasDependents) {
//	    var dep *domain.DependentsError
//	    errors.As(err, &dep)
//	    // dep.Impact 形如 {"child_menus": 3, "affected_roles": 2}
//	}
type DependentsError struct {
	Impact map[string]int64
}

func (e *DependentsError) Error() string {
	// 排序保证错误信息稳定，便于断言与日志比对
	keys := make([]string, 0, len(e.Impact))
	for k := range e.Impact {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, e.Impact[k]))
	}
	return "resource has dependents: " + strings.Join(parts, ", ")
}

func (e *DependentsError) Is(target error) bool { return target == ErrHasDependents }

// DuplicateError 携带冲突的字段名。
//
// 带上字段名是为了复用字段级错误的形状（errors[].field），
// 前端据此把「用户名已存在」高亮到对应的输入框上。
type DuplicateError struct {
	Field string
}

func (e *DuplicateError) Error() string { return "duplicate value for " + e.Field }

func (e *DuplicateError) Is(target error) bool { return target == ErrDuplicate }

// Duplicate 构造一个唯一字段冲突错误。
func Duplicate(field string) error { return &DuplicateError{Field: field} }

// HasDependents 构造一个影响面错误，仅当确实存在依赖（任一计数大于 0）时返回非 nil。
func HasDependents(impact map[string]int64) error {
	for _, n := range impact {
		if n > 0 {
			return &DependentsError{Impact: impact}
		}
	}
	return nil
}
