package service

import (
	"context"
	"fmt"

	"mini-ruoyi/internal/domain"
	"mini-ruoyi/internal/perm"
	"mini-ruoyi/internal/repository"
)

// Grants 是角色的授权集合。两个字段都会保证非 nil，前端不必特判。
type Grants struct {
	MenuIDs   []int64  `json:"menu_ids"`
	PermCodes []string `json:"perm_codes"`
}

type RoleService struct {
	repo *repository.RoleRepository
}

func NewRoleService(repo *repository.RoleRepository) *RoleService {
	return &RoleService{repo: repo}
}

func (s *RoleService) List(ctx context.Context, f repository.RoleFilter, page, pageSize int) (Page[domain.Role], error) {
	page, pageSize = normalizePage(page, pageSize)

	total, err := s.repo.Count(ctx, f)
	if err != nil {
		return Page[domain.Role]{}, fmt.Errorf("count roles: %w", err)
	}
	page, offset := clampPage(page, pageSize, total)
	list, err := s.repo.List(ctx, f, pageSize, offset)
	if err != nil {
		return Page[domain.Role]{}, fmt.Errorf("list roles: %w", err)
	}
	return Page[domain.Role]{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

func (s *RoleService) Get(ctx context.Context, id int64) (domain.Role, error) {
	role, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return domain.Role{}, fmt.Errorf("get role %d: %w", id, err)
	}
	return role, nil
}

func (s *RoleService) Create(ctx context.Context, role domain.Role) (domain.Role, error) {
	// 内置角色标识是保留的，不能通过接口创建，否则可以伪造一个超级管理员
	if role.Code == domain.RoleCodeAdmin {
		return domain.Role{}, fmt.Errorf("role code %q: %w", role.Code, domain.ErrProtected)
	}
	exists, err := s.repo.ExistsCode(ctx, role.Code, 0)
	if err != nil {
		return domain.Role{}, fmt.Errorf("check role code: %w", err)
	}
	if exists {
		return domain.Role{}, domain.Duplicate("code")
	}
	role.Status = orActive(role.Status)
	created, err := s.repo.Create(ctx, role)
	if err != nil {
		return domain.Role{}, fmt.Errorf("create role: %w", err)
	}
	return created, nil
}

// Update 不允许修改内置角色的 code（code 根本没在 UPDATE 语句里），
// 也不允许把内置角色停用——停用后无人能授权，且界面无法恢复。
func (s *RoleService) Update(ctx context.Context, role domain.Role) error {
	current, err := s.repo.GetByID(ctx, role.ID)
	if err != nil {
		return fmt.Errorf("get role %d: %w", role.ID, err)
	}
	if current.IsBuiltin() && role.Status != domain.StatusActive {
		return fmt.Errorf("role %d is builtin: %w", role.ID, domain.ErrProtected)
	}
	if err := s.repo.Update(ctx, role); err != nil {
		return fmt.Errorf("update role %d: %w", role.ID, err)
	}
	return nil
}

// Delete 删除角色。
//
// 内置角色一律拒绝。其余角色在 cascade=false 时先算影响面：删除会级联清空
// sys_user_roles / sys_role_menus / sys_role_perms，持有该角色的用户会立刻少掉一批能力。
func (s *RoleService) Delete(ctx context.Context, id int64, cascade bool) error {
	role, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get role %d: %w", id, err)
	}
	if role.IsBuiltin() {
		return fmt.Errorf("role %d is builtin: %w", id, domain.ErrProtected)
	}
	if !cascade {
		impact, err := s.Impact(ctx, id)
		if err != nil {
			return err
		}
		if err := domain.HasDependents(impact); err != nil {
			return err
		}
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete role %d: %w", id, err)
	}
	return nil
}

// Grants 返回角色当前的菜单授权与权限码，给授权界面回填。
func (s *RoleService) Grants(ctx context.Context, id int64) (Grants, error) {
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return Grants{}, fmt.Errorf("get role %d: %w", id, err)
	}
	menuIDs, err := s.repo.MenuIDs(ctx, id)
	if err != nil {
		return Grants{}, fmt.Errorf("list menu grants of role %d: %w", id, err)
	}
	codes, err := s.repo.PermCodesOfRole(ctx, id)
	if err != nil {
		return Grants{}, fmt.Errorf("list perm grants of role %d: %w", id, err)
	}
	if menuIDs == nil {
		menuIDs = []int64{}
	}
	if codes == nil {
		codes = []string{}
	}
	return Grants{MenuIDs: menuIDs, PermCodes: codes}, nil
}

// SetGrants 覆盖式重设角色的授权。
//
// 权限码先在这里校验：库里不允许出现代码未声明的码，否则它就成了一条永远不生效
// 的授权，用户以为给了权限其实没给。启动时的全量校验是最后一道防线，这里先拦住。
func (s *RoleService) SetGrants(ctx context.Context, roleID int64, g Grants) error {
	role, err := s.repo.GetByID(ctx, roleID)
	if err != nil {
		return fmt.Errorf("get role %d: %w", roleID, err)
	}
	// 内置管理员的权限是隐式的，写进库反而会造成「库里有一堆用不上的授权」
	if role.IsBuiltin() {
		return fmt.Errorf("role %d is builtin: %w", roleID, domain.ErrProtected)
	}
	if unknown := perm.Unknown(g.PermCodes); len(unknown) > 0 {
		return fmt.Errorf("未知权限码 %v: %w", unknown, domain.ErrInvalidPermCode)
	}
	if err := s.repo.ReplaceGrants(ctx, roleID, g.MenuIDs, g.PermCodes); err != nil {
		return fmt.Errorf("replace grants of role %d: %w", roleID, err)
	}
	return nil
}

func (s *RoleService) Impact(ctx context.Context, id int64) (map[string]int64, error) {
	users, err := s.repo.CountAffectedUsers(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("count affected users of role %d: %w", id, err)
	}
	return map[string]int64{"affected_users": users}, nil
}
