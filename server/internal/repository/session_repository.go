package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"mini-ruoyi/internal/domain"
)

type SessionRepository struct {
	db *sql.DB
}

func NewSessionRepository(db *sql.DB) *SessionRepository {
	return &SessionRepository{db: db}
}

const sessionColumns = `token_hash, user_id, created_at, expires_at, last_seen_at,
	csrf_token, ip, user_agent`

func scanSession(s interface{ Scan(...any) error }) (domain.Session, error) {
	var item domain.Session
	err := s.Scan(&item.TokenHash, &item.UserID, &item.CreatedAt, &item.ExpiresAt,
		&item.LastSeenAt, &item.CSRFToken, &item.IP, &item.UserAgent)
	return item, err
}

func (r *SessionRepository) Create(ctx context.Context, s domain.Session) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO sys_sessions
			(token_hash, user_id, expires_at, csrf_token, ip, user_agent)
		VALUES (?, ?, ?, ?, ?, ?)`,
		s.TokenHash, s.UserID, toDBTime(s.ExpiresAt), s.CSRFToken, s.IP, s.UserAgent)
	return err
}

func (r *SessionRepository) GetByTokenHash(ctx context.Context, hash string) (domain.Session, error) {
	item, err := scanSession(r.db.QueryRowContext(ctx,
		`SELECT `+sessionColumns+` FROM sys_sessions WHERE token_hash = ?`, hash))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Session{}, domain.ErrNotFound
	}
	return item, err
}

func (r *SessionRepository) Touch(ctx context.Context, hash string, seenAt time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE sys_sessions SET last_seen_at = ? WHERE token_hash = ?`, toDBTime(seenAt), hash)
	return err
}

func (r *SessionRepository) Delete(ctx context.Context, hash string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM sys_sessions WHERE token_hash = ?`, hash)
	return err
}

// List 返回未过期的会话，最近活跃的排在前面。
func (r *SessionRepository) List(ctx context.Context, limit, offset int) ([]domain.SessionView, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT s.token_hash, s.user_id, s.created_at, s.expires_at, s.last_seen_at,
		       s.csrf_token, s.ip, s.user_agent, u.username, u.nickname
		FROM sys_sessions s
		JOIN sys_users u ON u.id = s.user_id
		WHERE s.expires_at > ?
		ORDER BY s.last_seen_at DESC, s.token_hash
		LIMIT ? OFFSET ?`, toDBTime(time.Now()), limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]domain.SessionView, 0, limit)
	for rows.Next() {
		var v domain.SessionView
		if err := rows.Scan(&v.TokenHash, &v.UserID, &v.CreatedAt, &v.ExpiresAt, &v.LastSeenAt,
			&v.CSRFToken, &v.IP, &v.UserAgent, &v.Username, &v.Nickname); err != nil {
			return nil, err
		}
		list = append(list, v)
	}
	return list, rows.Err()
}

// Count 返回未过期的会话数。
func (r *SessionRepository) Count(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sys_sessions WHERE expires_at > ?`, toDBTime(time.Now())).Scan(&n)
	return n, err
}

// DeleteByUserExcept 踢掉某用户除 keep 之外的全部会话。
//
// 用于「修改自己的密码」：当前这条要留着，否则用户改完密码立刻被登出，
// 他会以为改失败了。其他会话必须踢掉——改密码的常见动机就是「怀疑密码泄露」。
func (r *SessionRepository) DeleteByUserExcept(ctx context.Context, userID int64, keepTokenHash string) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM sys_sessions WHERE user_id = ? AND token_hash <> ?`, userID, keepTokenHash)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteByUser 踢掉某个用户的全部会话。修改密码或停用账号后应当调用它。
func (r *SessionRepository) DeleteByUser(ctx context.Context, userID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM sys_sessions WHERE user_id = ?`, userID)
	return err
}

// DeleteExpired 清理过期会话，返回删除条数。
//
// 有两个调用方：登录时顺手清一次（登录是低频操作，白捡的），
// 以及定时任务每小时清一次（保证长期没人登录时也会被清）。
func (r *SessionRepository) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM sys_sessions WHERE expires_at < ?`, toDBTime(now))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
