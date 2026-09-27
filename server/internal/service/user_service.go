package service

import (
	"context"
	"fmt"

	"mini-ruoyi/internal/auth"
	"mini-ruoyi/internal/domain"
	"mini-ruoyi/internal/repository"
)

type UserService struct {
	repo     *repository.UserRepository
	sessions *auth.SessionService
}

func NewUserService(repo *repository.UserRepository, sessions *auth.SessionService) *UserService {
	return &UserService{repo: repo, sessions: sessions}
}

// CreateUserInput 是建用户的输入。Password 是明文，只在本层存在。
type CreateUserInput struct {
	Username string
	Password string
	Nickname string
	Mobile   string
	Email    string
	Status   string
}

func (s *UserService) List(ctx context.Context, f repository.UserFilter, page, pageSize int) (Page[domain.User], error) {
	page, pageSize = normalizePage(page, pageSize)

	total, err := s.repo.Count(ctx, f)
	if err != nil {
		return Page[domain.User]{}, fmt.Errorf("count users: %w", err)
	}
	page, offset := clampPage(page, pageSize, total)
	list, err := s.repo.List(ctx, f, pageSize, offset)
	if err != nil {
		return Page[domain.User]{}, fmt.Errorf("list users: %w", err)
	}
	return Page[domain.User]{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

// UserDetail 是编辑表单需要的完整信息：用户本体 + 已绑定的角色。
type UserDetail struct {
	domain.User
	RoleIDs []int64 `json:"role_ids"`
}

func (s *UserService) GetDetail(ctx context.Context, id int64) (UserDetail, error) {
	u, err := s.Get(ctx, id)
	if err != nil {
		return UserDetail{}, err
	}
	roleIDs, err := s.repo.RoleIDs(ctx, id)
	if err != nil {
		return UserDetail{}, fmt.Errorf("list roles of user %d: %w", id, err)
	}
	if roleIDs == nil {
		roleIDs = []int64{}
	}
	return UserDetail{User: u, RoleIDs: roleIDs}, nil
}

// Create 建用户。密码在这里哈希，明文不会离开这一层。
func (s *UserService) Create(ctx context.Context, in CreateUserInput) (domain.User, error) {
	exists, err := s.repo.ExistsUsername(ctx, in.Username, 0)
	if err != nil {
		return domain.User{}, fmt.Errorf("check username: %w", err)
	}
	if exists {
		return domain.User{}, domain.Duplicate("username")
	}

	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		// bcrypt 的 72 字节上限应在请求校验层挡住，走到这里是编程错误
		return domain.User{}, fmt.Errorf("hash password: %w", err)
	}

	created, err := s.repo.Create(ctx, domain.User{
		Status:   orActive(in.Status),
		Username: in.Username,
		Password: hash,
		Nickname: in.Nickname,
		Mobile:   in.Mobile,
		Email:    in.Email,
	})
	if err != nil {
		return domain.User{}, fmt.Errorf("create user: %w", err)
	}
	return created, nil
}

// ResetPassword 重置他人密码，并踢掉该用户的全部会话——
// 改密码的常见动机就是「怀疑账号被盗」，不踢会话等于没改。
func (s *UserService) ResetPassword(ctx context.Context, id int64, plain string) error {
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return fmt.Errorf("get user %d: %w", id, err)
	}
	hash, err := auth.HashPassword(plain)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if err := s.repo.UpdatePassword(ctx, id, hash); err != nil {
		return fmt.Errorf("update password of user %d: %w", id, err)
	}
	if err := s.sessions.RevokeUser(ctx, id); err != nil {
		return err
	}
	return nil
}

func (s *UserService) Get(ctx context.Context, id int64) (domain.User, error) {
	u, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return domain.User{}, fmt.Errorf("get user %d: %w", id, err)
	}
	return u, nil
}

// Update 保存用户资料。
//
// 若这次会把一个管理员停用，必须确认还有别的在用管理员——被停用的管理员登不进来，
// 效果等同于删除，同样会把系统锁死。
func (s *UserService) Update(ctx context.Context, u domain.User) error {
	current, err := s.repo.GetByID(ctx, u.ID)
	if err != nil {
		return fmt.Errorf("get user %d: %w", u.ID, err)
	}

	beingDisabled := current.Status == domain.StatusActive && u.Status != domain.StatusActive
	if beingDisabled {
		if err := s.guardLastAdmin(ctx, u.ID); err != nil {
			return err
		}
	}

	if err := s.repo.UpdateProfile(ctx, u); err != nil {
		return fmt.Errorf("update user %d: %w", u.ID, err)
	}
	return nil
}

// Delete 删除用户。
//
// 两道守卫，都是「一旦发生就只能手工改库」的情况：
//   - 不能删自己：当前会话会立刻失效
//   - 不能删最后一个在用管理员：没人能再进后台
func (s *UserService) Delete(ctx context.Context, id, currentUserID int64) error {
	if id == currentUserID {
		return fmt.Errorf("delete user %d: %w", id, domain.ErrCannotDeleteSelf)
	}
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return fmt.Errorf("get user %d: %w", id, err)
	}
	if err := s.guardLastAdmin(ctx, id); err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete user %d: %w", id, err)
	}
	return nil
}

// SetRoles 重设用户的角色绑定。
func (s *UserService) SetRoles(ctx context.Context, userID int64, roleIDs []int64) error {
	if _, err := s.repo.GetByID(ctx, userID); err != nil {
		return fmt.Errorf("get user %d: %w", userID, err)
	}
	if err := s.repo.ReplaceRoles(ctx, userID, roleIDs); err != nil {
		return fmt.Errorf("set roles of user %d: %w", userID, err)
	}
	return nil
}

// guardLastAdmin 只在目标用户确实持有内置 admin 角色时才检查，
// 避免删除普通用户时抛出让人困惑的「最后一个管理员」错误。
func (s *UserService) guardLastAdmin(ctx context.Context, userID int64) error {
	isAdmin, err := s.repo.HasRole(ctx, userID, domain.RoleCodeAdmin)
	if err != nil {
		return fmt.Errorf("check admin role of user %d: %w", userID, err)
	}
	if !isAdmin {
		return nil
	}
	others, err := s.repo.CountOtherAdmins(ctx, userID)
	if err != nil {
		return fmt.Errorf("count other admins: %w", err)
	}
	if others == 0 {
		return fmt.Errorf("user %d is the last admin: %w", userID, domain.ErrLastAdmin)
	}
	return nil
}
