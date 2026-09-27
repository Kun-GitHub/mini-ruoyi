package repository

import (
	"context"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"mini-ruoyi/internal/domain"
)

func newSessionRepo(t *testing.T) (*SessionRepository, *UserRepository, context.Context) {
	t.Helper()

	db, err := NewDB(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return NewSessionRepository(db), NewUserRepository(db), ctx
}

func adminID(t *testing.T, users *UserRepository, ctx context.Context) int64 {
	t.Helper()
	u, err := users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("取 admin 用户: %v", err)
	}
	return u.ID
}

// TestSessionTimesUseCanonicalFormat 是这个文件里最重要的用例。
//
// 直接把 time.Time 交给驱动时，写出来的是 t.String()：
//
//	2026-10-04 16:44:22.094239 +0800 CST m=+604802.023486084
//
// 而 DEFAULT CURRENT_TIMESTAMP 写的是：
//
//	2026-09-27 08:44:22
//
// 同一列两种格式，SQLite 又是逐字节比较 TEXT，于是按时间过滤的语句会静默出错。
// 这里断言存储格式与原生的 CURRENT_TIMESTAMP 完全一致。
func TestSessionTimesUseCanonicalFormat(t *testing.T) {
	sessions, users, ctx := newSessionRepo(t)

	expires := time.Date(2026, 10, 4, 16, 44, 22, 0, time.UTC)
	if err := sessions.Create(ctx, domain.Session{
		TokenHash: "hash-format", UserID: adminID(t, users, ctx),
		ExpiresAt: expires, CSRFToken: "csrf",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	canonical := regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`)

	for _, col := range []string{"created_at", "expires_at", "last_seen_at"} {
		var raw string
		if err := queryOne(ctx, sessions, `SELECT CAST(`+col+` AS TEXT) FROM sys_sessions`, &raw); err != nil {
			t.Fatal(err)
		}
		if !canonical.MatchString(raw) {
			t.Errorf("%s 的存储格式是 %q，不符合 YYYY-MM-DD HH:MM:SS", col, raw)
		}
		if regexp.MustCompile(`m=|\+0800|CST|\.\d`).MatchString(raw) {
			t.Errorf("%s 里混进了时区名或单调时钟读数: %q", col, raw)
		}
	}

	// DEFAULT CURRENT_TIMESTAMP 与 Go 绑定的值必须能直接比较
	var ordering string
	if err := queryOne(ctx, sessions,
		`SELECT CASE WHEN created_at <= expires_at THEN 'ok' ELSE 'broken' END FROM sys_sessions`,
		&ordering); err != nil {
		t.Fatal(err)
	}
	if ordering != "ok" {
		t.Error("created_at 与 expires_at 无法按字符串正确比较，两种时间格式不一致")
	}
}

// queryOne 是只取一列的最小查询辅助，避免为几个断言引入更多结构体。
func queryOne(ctx context.Context, r *SessionRepository, q string, dest any) error {
	return r.db.QueryRowContext(ctx, q).Scan(dest)
}

// TestSessionTimeRoundTrip 确认写进去再读出来是同一个时刻。
func TestSessionTimeRoundTrip(t *testing.T) {
	sessions, users, ctx := newSessionRepo(t)

	expires := time.Now().UTC().Add(7 * 24 * time.Hour).Truncate(time.Second)
	if err := sessions.Create(ctx, domain.Session{
		TokenHash: "hash-roundtrip", UserID: adminID(t, users, ctx),
		ExpiresAt: expires, CSRFToken: "csrf",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := sessions.GetByTokenHash(ctx, "hash-roundtrip")
	if err != nil {
		t.Fatalf("GetByTokenHash: %v", err)
	}
	if !got.ExpiresAt.Equal(expires) {
		t.Errorf("ExpiresAt 往返后 = %v，期望 %v", got.ExpiresAt, expires)
	}
}

// TestDeleteExpired 覆盖过期清理。它同时是时间格式的一道防线：
// 如果 expires_at 与传入的 now 格式不一致，字符串比较会得出错误结论。
func TestDeleteExpired(t *testing.T) {
	sessions, users, ctx := newSessionRepo(t)
	uid := adminID(t, users, ctx)
	now := time.Now().UTC()

	mk := func(hash string, expiresAt time.Time) {
		t.Helper()
		if err := sessions.Create(ctx, domain.Session{
			TokenHash: hash, UserID: uid, ExpiresAt: expiresAt, CSRFToken: "csrf",
		}); err != nil {
			t.Fatalf("Create %s: %v", hash, err)
		}
	}
	mk("expired-1h", now.Add(-time.Hour))
	mk("expired-8h", now.Add(-8*time.Hour)) // 跨时区偏移的边界量级
	mk("valid-1h", now.Add(time.Hour))
	mk("valid-7d", now.Add(7*24*time.Hour))

	removed, err := sessions.DeleteExpired(ctx, now)
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if removed != 2 {
		t.Errorf("DeleteExpired 删除了 %d 条，期望 2（两条已过期）", removed)
	}

	for _, tc := range []struct {
		hash string
		want bool // 期望仍存在
	}{
		{"expired-1h", false},
		{"expired-8h", false},
		{"valid-1h", true},
		{"valid-7d", true},
	} {
		_, err := sessions.GetByTokenHash(ctx, tc.hash)
		exists := err == nil
		if exists != tc.want {
			t.Errorf("%s 存在性 = %v，期望 %v", tc.hash, exists, tc.want)
		}
	}
}

// TestDeleteByUserRevokesAllSessions 覆盖「踢人」：停用或改密码后要一次清干净。
func TestDeleteByUserRevokesAllSessions(t *testing.T) {
	sessions, users, ctx := newSessionRepo(t)
	uid := adminID(t, users, ctx)
	expires := time.Now().UTC().Add(time.Hour)

	for _, h := range []string{"s1", "s2", "s3"} {
		if err := sessions.Create(ctx, domain.Session{
			TokenHash: h, UserID: uid, ExpiresAt: expires, CSRFToken: "csrf",
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := sessions.DeleteByUser(ctx, uid); err != nil {
		t.Fatalf("DeleteByUser: %v", err)
	}
	for _, h := range []string{"s1", "s2", "s3"} {
		if _, err := sessions.GetByTokenHash(ctx, h); err == nil {
			t.Errorf("会话 %s 未被清除", h)
		}
	}
}

// TestDeletingUserCascadesSessions 确认外键级联真的生效——
// 删掉用户后会话必须跟着消失，否则会留下引用空用户的「幽灵会话」。
func TestDeletingUserCascadesSessions(t *testing.T) {
	sessions, users, ctx := newSessionRepo(t)

	u, err := users.Create(ctx, domain.User{
		Status: domain.StatusActive, Username: "temp", Password: "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sessions.Create(ctx, domain.Session{
		TokenHash: "cascade", UserID: u.ID,
		ExpiresAt: time.Now().UTC().Add(time.Hour), CSRFToken: "csrf",
	}); err != nil {
		t.Fatal(err)
	}

	if err := users.Delete(ctx, u.ID); err != nil {
		t.Fatalf("Delete user: %v", err)
	}
	if _, err := sessions.GetByTokenHash(ctx, "cascade"); err == nil {
		t.Error("删除用户后会话仍然存在，外键级联没有生效")
	}
}
