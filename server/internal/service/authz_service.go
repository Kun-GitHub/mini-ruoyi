package service

import (
	"context"
	"fmt"

	"mini-ruoyi/internal/domain"
	"mini-ruoyi/internal/perm"
	"mini-ruoyi/internal/repository"
)

// AuthzService 负责把一个用户解析成「他能不能做某件事」。
type AuthzService struct {
	users *repository.UserRepository
	roles *repository.RoleRepository
}

func NewAuthzService(users *repository.UserRepository, roles *repository.RoleRepository) *AuthzService {
	return &AuthzService{users: users, roles: roles}
}

// Identity 是鉴权中间件缓存在请求上下文里的东西，
// 一次请求只解析一次，权限中间件直接读它。
type Identity struct {
	User domain.User
	// IsAdmin 为 true 时跳过所有权限码检查，见 docs/schema.md §8.2。
	IsAdmin bool
	// Perms 只包含显式授予的权限码；IsAdmin 为 true 时它可能是空的，这不代表没权限。
	Perms map[string]bool
}

// Can 判断身份是否拥有某个权限码。
func (i Identity) Can(code string) bool {
	return i.IsAdmin || i.Perms[code]
}

// IdentityOf 解析用户的角色与权限。
//
// 每个请求两次查询（是否内置管理员 + 权限码集合）。管理后台的 QPS 是个位数，
// 这个量级不需要缓存；等真的有性能压力了再加进程内缓存 + 版本号失效。
func (s *AuthzService) IdentityOf(ctx context.Context, user domain.User) (Identity, error) {
	isAdmin, err := s.users.HasRole(ctx, user.ID, domain.RoleCodeAdmin)
	if err != nil {
		return Identity{}, fmt.Errorf("check admin role of user %d: %w", user.ID, err)
	}

	// 内置管理员：权限码不落库，直接把全部已声明的码铺满，
	// 这样前端只需一种判断（perms.includes(code)），不用再分支处理 is_admin
	if isAdmin {
		perms := make(map[string]bool, len(perm.All()))
		for _, c := range perm.All() {
			perms[string(c)] = true
		}
		return Identity{User: user, IsAdmin: true, Perms: perms}, nil
	}

	codes, err := s.roles.ListPermCodesOfUser(ctx, user.ID)
	if err != nil {
		return Identity{}, fmt.Errorf("list perms of user %d: %w", user.ID, err)
	}
	perms := make(map[string]bool, len(codes))
	for _, c := range codes {
		perms[c] = true
	}
	return Identity{User: user, Perms: perms}, nil
}
