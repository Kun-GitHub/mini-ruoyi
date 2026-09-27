package service

import (
	"context"
	"fmt"

	"mini-ruoyi/internal/domain"
	"mini-ruoyi/internal/repository"
)

type MenuService struct {
	repo *repository.MenuRepository
}

func NewMenuService(repo *repository.MenuRepository) *MenuService {
	return &MenuService{repo: repo}
}

// Tree 返回完整菜单树（含未启用项）。给菜单管理界面用。
func (s *MenuService) Tree(ctx context.Context) ([]*domain.MenuNode, error) {
	flat, err := s.repo.List(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("list menus: %w", err)
	}
	return buildMenuTree(flat), nil
}

// TreeForIdentity 返回当前用户可见的菜单树，给前端生成导航与动态路由。
//
//   - 内置管理员：全部启用的菜单
//   - 其他用户：只包含角色被授权的菜单（父节点未被授权时，子节点会因为找不到父节点
//     而各自成为根节点，这比静默丢掉它们更容易被发现）
func (s *MenuService) TreeForIdentity(ctx context.Context, id Identity) ([]*domain.MenuNode, error) {
	var (
		flat []domain.Menu
		err  error
	)
	if id.IsAdmin {
		flat, err = s.repo.List(ctx, true)
	} else {
		flat, err = s.repo.ListForUser(ctx, id.User.ID)
	}
	if err != nil {
		return nil, fmt.Errorf("list menus for user: %w", err)
	}
	return buildMenuTree(flat), nil
}

func (s *MenuService) Get(ctx context.Context, id int64) (domain.Menu, error) {
	m, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return domain.Menu{}, fmt.Errorf("get menu %d: %w", id, err)
	}
	return m, nil
}

// Create 校验父节点存在且类型合法。
func (s *MenuService) Create(ctx context.Context, m domain.Menu) (domain.Menu, error) {
	if err := s.validateParent(ctx, m.ParentID); err != nil {
		return domain.Menu{}, err
	}
	m.Status = orActive(m.Status)
	created, err := s.repo.Create(ctx, m)
	if err != nil {
		return domain.Menu{}, fmt.Errorf("create menu: %w", err)
	}
	return created, nil
}

// Update 额外挡住「把菜单挂到自己的后代下面」——那会形成环，
// 让整棵子树从界面上消失，而且无法在界面上恢复。
func (s *MenuService) Update(ctx context.Context, m domain.Menu) error {
	if m.ParentID != nil && *m.ParentID == m.ID {
		return fmt.Errorf("menu %d: %w", m.ID, domain.ErrInvalidParent)
	}
	if err := s.validateParent(ctx, m.ParentID); err != nil {
		return err
	}
	if m.ParentID != nil {
		cycle, err := s.createsCycle(ctx, m.ID, *m.ParentID)
		if err != nil {
			return err
		}
		if cycle {
			return fmt.Errorf("menu %d: %w", m.ID, domain.ErrInvalidParent)
		}
	}
	if err := s.repo.Update(ctx, m); err != nil {
		return fmt.Errorf("update menu %d: %w", m.ID, err)
	}
	return nil
}

// Delete 删除菜单。
//
// cascade 为 false 时先算影响面，存在子菜单或被角色引用就返回 *domain.DependentsError，
// 由 handler 映射成 409 交给前端弹确认框。cascade 为 true 时直接删——数据库上的
// 外键是 ON DELETE CASCADE，整棵子树会一起消失。
func (s *MenuService) Delete(ctx context.Context, id int64, cascade bool) error {
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return fmt.Errorf("get menu %d: %w", id, err)
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
		return fmt.Errorf("delete menu %d: %w", id, err)
	}
	return nil
}

// Impact 计算删除该菜单的影响面。
//
// affected_roles 必须算上后代菜单的授权：级联删除会把子树一并带走，
// 只数自己的直接引用会低报影响面，用户就会在不知情的情况下少了一批授权。
func (s *MenuService) Impact(ctx context.Context, id int64) (map[string]int64, error) {
	children, err := s.repo.CountDescendants(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("count descendants of menu %d: %w", id, err)
	}
	roles, err := s.repo.CountAffectedRoles(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("count affected roles of menu %d: %w", id, err)
	}
	return map[string]int64{
		"child_menus":    children,
		"affected_roles": roles,
	}, nil
}

func (s *MenuService) validateParent(ctx context.Context, parentID *int64) error {
	if parentID == nil {
		return nil
	}
	parent, err := s.repo.GetByID(ctx, *parentID)
	if err != nil {
		return fmt.Errorf("parent menu %d: %w", *parentID, err)
	}
	// 只有目录能挂子节点。往菜单下挂子菜单在界面上无法表达。
	if parent.MenuType != domain.MenuTypeDirectory {
		return fmt.Errorf("parent menu %d is not a directory: %w", *parentID, domain.ErrInvalidParent)
	}
	return nil
}

// createsCycle 判断把 nodeID 挂到 newParentID 下是否会形成环：
// 从 newParentID 往根走，若遇到 nodeID 则说明 newParentID 是 nodeID 的后代。
func (s *MenuService) createsCycle(ctx context.Context, nodeID, newParentID int64) (bool, error) {
	flat, err := s.repo.List(ctx, false)
	if err != nil {
		return false, fmt.Errorf("list menus: %w", err)
	}
	byID := make(map[int64]domain.Menu, len(flat))
	for _, m := range flat {
		byID[m.ID] = m
	}

	for cur := newParentID; ; {
		if cur == nodeID {
			return true, nil
		}
		parent, ok := byID[cur]
		if !ok || parent.ParentID == nil {
			return false, nil
		}
		cur = *parent.ParentID
	}
}

// buildMenuTree 把平铺列表组装成树。
//
// 父节点找不到时把该节点当作根节点处理：外键本应保证这种情况不出现，
// 但真出现了也不该让菜单静默消失——那会让管理员完全看不到它有问题的行。
func buildMenuTree(flat []domain.Menu) []*domain.MenuNode {
	nodes := make(map[int64]*domain.MenuNode, len(flat))
	for _, m := range flat {
		nodes[m.ID] = &domain.MenuNode{Menu: m, Children: []*domain.MenuNode{}}
	}

	roots := make([]*domain.MenuNode, 0)
	for _, m := range flat {
		node := nodes[m.ID]
		if m.ParentID == nil {
			roots = append(roots, node)
			continue
		}
		parent, ok := nodes[*m.ParentID]
		if !ok {
			roots = append(roots, node)
			continue
		}
		parent.Children = append(parent.Children, node)
	}
	return roots
}
