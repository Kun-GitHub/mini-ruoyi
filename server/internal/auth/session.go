package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"time"

	"golang.org/x/crypto/bcrypt"

	"mini-ruoyi/internal/domain"
	"mini-ruoyi/internal/repository"
)

// SessionTTL 是会话有效期。
//
// 固定 7 天，不做滑动续期。取舍：
//
//   - 滑动续期会让一个长期不关的页面永远不会失效，安全性反而更差；固定期限下
//     即使 cookie 被窃取，它也会自然到期
//   - 7 天意味着每天使用的管理员大约每周重登一次，对管理后台可以接受
//
// 长有效期带来的风险由其他机制兜住：cookie 是 HttpOnly（XSS 偷不走）、
// SameSite=Lax + CSRF 头（跨站写操作无效）、会话可被服务端立即删除
// （登出、停用用户、改密码），last_seen_at 也留下了使用痕迹。
const SessionTTL = 7 * 24 * time.Hour

// touchInterval 是刷新 last_seen_at 的最小间隔。
//
// 不能每个请求都写：SQLite 是单写者，每次请求写一行会让所有请求在写锁上排队。
// 管理后台看不到 5 分钟的精度差异，但写量从「每请求」降到「每会话每 5 分钟」。
const touchInterval = 5 * time.Minute

type SessionService struct {
	sessions *repository.SessionRepository
	users    *repository.UserRepository
	now      func() time.Time
}

func NewSessionService(sessions *repository.SessionRepository, users *repository.UserRepository) *SessionService {
	return &SessionService{sessions: sessions, users: users, now: time.Now}
}

// LoginResult 只在登录那一刻持有明文 token，之后服务端只认哈希。
type LoginResult struct {
	Token   string
	Session domain.Session
	User    domain.User
}

// Login 校验凭据并创建会话。
func (s *SessionService) Login(ctx context.Context, username, password, ip, userAgent string) (LoginResult, error) {
	// 统一取 UTC：库里存的是 UTC（见 repository.toDBTime），
	// 服务端持有的时间保持同一时区可以避免响应里出现两种偏移量
	now := s.now().UTC()

	// 登录是低频操作，顺手清理过期会话。这只是「白捡的一次清理」，
	// 真正保证长期清理的是定时任务（见 internal/job）。
	if _, err := s.sessions.DeleteExpired(ctx, now); err != nil {
		return LoginResult{}, fmt.Errorf("cleanup expired sessions: %w", err)
	}

	user, err := s.users.GetByUsername(ctx, username)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		// 用户名不存在也走一次 bcrypt，让耗时与「密码错误」一致，避免用户名枚举
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return LoginResult{}, domain.ErrBadCredentials
	case err != nil:
		return LoginResult{}, fmt.Errorf("get user by username: %w", err)
	}

	if !VerifyPassword(user.Password, password) {
		return LoginResult{}, domain.ErrBadCredentials
	}
	if !user.IsActive() {
		return LoginResult{}, domain.ErrAccountDisabled
	}

	token, hash, err := newToken()
	if err != nil {
		return LoginResult{}, fmt.Errorf("generate session token: %w", err)
	}
	csrf, err := newSecret()
	if err != nil {
		return LoginResult{}, fmt.Errorf("generate csrf token: %w", err)
	}

	session := domain.Session{
		TokenHash: hash,
		UserID:    user.ID,
		CreatedAt: now,
		ExpiresAt: now.Add(SessionTTL),
		CSRFToken: csrf,
		IP:        ip,
		UserAgent: truncate(userAgent, 255),
	}
	if err := s.sessions.Create(ctx, session); err != nil {
		return LoginResult{}, fmt.Errorf("create session: %w", err)
	}

	// 记录最近一次登录。写失败不影响登录本身——登录信息是展示用的，
	// 不该因为它写不进去而把用户挡在外面。
	if err := s.users.SetLoginInfo(ctx, user.ID, ip, now); err != nil {
		log.Printf("记录用户 %d 的登录信息失败: %v", user.ID, err)
	}

	return LoginResult{Token: token, Session: session, User: user}, nil
}

// Resolve 用 cookie 里的明文 token 换出用户与会话。
//
// 会话失效、已过期、用户被删除或被停用时都返回错误，并顺手删掉这条会话——
// 「停用用户」要立刻生效，不能等 token 自己过期。
func (s *SessionService) Resolve(ctx context.Context, token string) (domain.User, domain.Session, error) {
	if token == "" {
		return domain.User{}, domain.Session{}, domain.ErrUnauthorized
	}

	session, err := s.sessions.GetByTokenHash(ctx, hashToken(token))
	if errors.Is(err, domain.ErrNotFound) {
		return domain.User{}, domain.Session{}, domain.ErrUnauthorized
	}
	if err != nil {
		return domain.User{}, domain.Session{}, fmt.Errorf("get session: %w", err)
	}

	now := s.now().UTC()
	if session.Expired(now) {
		_ = s.sessions.Delete(ctx, session.TokenHash)
		return domain.User{}, domain.Session{}, domain.ErrUnauthorized
	}

	user, err := s.users.GetByID(ctx, session.UserID)
	if errors.Is(err, domain.ErrNotFound) {
		_ = s.sessions.Delete(ctx, session.TokenHash)
		return domain.User{}, domain.Session{}, domain.ErrUnauthorized
	}
	if err != nil {
		return domain.User{}, domain.Session{}, fmt.Errorf("get session user: %w", err)
	}
	if !user.IsActive() {
		_ = s.sessions.Delete(ctx, session.TokenHash)
		return domain.User{}, domain.Session{}, domain.ErrAccountDisabled
	}

	// 节流刷新，见 touchInterval 的说明
	if now.Sub(session.LastSeenAt) >= touchInterval {
		if err := s.sessions.Touch(ctx, session.TokenHash, now); err != nil {
			return domain.User{}, domain.Session{}, fmt.Errorf("touch session: %w", err)
		}
		session.LastSeenAt = now
	}

	return user, session, nil
}

func (s *SessionService) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	if err := s.sessions.Delete(ctx, hashToken(token)); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// List 返回在线会话（未过期的），最近活跃的在前。
func (s *SessionService) List(ctx context.Context, page, pageSize int) (SessionPage, error) {
	total, err := s.sessions.Count(ctx)
	if err != nil {
		return SessionPage{}, fmt.Errorf("count sessions: %w", err)
	}

	offset := (page - 1) * pageSize
	list, err := s.sessions.List(ctx, pageSize, offset)
	if err != nil {
		return SessionPage{}, fmt.Errorf("list sessions: %w", err)
	}

	return SessionPage{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

// SessionPage 是在线会话的分页结果。字段与其它列表接口保持一致，
// 前端的分页逻辑可以复用。
type SessionPage struct {
	List     []domain.SessionView `json:"list"`
	Total    int64                `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
}

// Kick 踢掉一条会话。
//
// 不能踢自己当前这条：效果就是突然被登出，用户会莫名其妙，
// 而且他本来就有「退出登录」按钮。与「不能删自己」同理。
func (s *SessionService) Kick(ctx context.Context, tokenHash, currentTokenHash string) error {
	if tokenHash == "" {
		return domain.ErrNotFound
	}
	if tokenHash == currentTokenHash {
		return domain.ErrCannotKickSelf
	}

	// 先确认存在：Delete 对不存在的行也返回成功，
	// 那样「踢成功了」和「本来就没有」就分不出来
	if _, err := s.sessions.GetByTokenHash(ctx, tokenHash); err != nil {
		return fmt.Errorf("get session: %w", err)
	}
	if err := s.sessions.Delete(ctx, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// CleanupExpired 清理过期会话，返回清理条数。供定时任务调用。
func (s *SessionService) CleanupExpired(ctx context.Context) (int64, error) {
	n, err := s.sessions.DeleteExpired(ctx, s.now().UTC())
	if err != nil {
		return 0, fmt.Errorf("cleanup expired sessions: %w", err)
	}
	return n, nil
}

// RevokeOthers 踢掉某用户除 keepTokenHash 之外的全部会话，返回踢掉几条。
func (s *SessionService) RevokeOthers(ctx context.Context, userID int64, keepTokenHash string) (int64, error) {
	n, err := s.sessions.DeleteByUserExcept(ctx, userID, keepTokenHash)
	if err != nil {
		return 0, fmt.Errorf("revoke other sessions of user %d: %w", userID, err)
	}
	return n, nil
}

// RevokeUser 踢掉某个用户的全部会话。改密码、停用账号后必须调用。
//
// 先确认用户存在，理由与 Kick 相同：DeleteByUser 对不存在的用户也返回成功，
// 那样「这个人已经不在了」和「踢干净了」就分不出来——会话列表那个按钮上会弹一个假的成功提示。
// 注意「用户存在、只是当下没有会话」是正常情况，不是错误。
func (s *SessionService) RevokeUser(ctx context.Context, userID int64) error {
	if _, err := s.users.GetByID(ctx, userID); err != nil {
		return fmt.Errorf("get user %d: %w", userID, err)
	}
	if err := s.sessions.DeleteByUser(ctx, userID); err != nil {
		return fmt.Errorf("revoke sessions of user %d: %w", userID, err)
	}
	return nil
}

// newToken 返回（明文 token, 落库用的哈希）。明文只出现在响应里，服务端只存哈希。
func newToken() (string, string, error) {
	raw, err := newSecret()
	if err != nil {
		return "", "", err
	}
	return raw, hashToken(raw), nil
}

func newSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
