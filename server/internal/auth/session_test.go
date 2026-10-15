// 有问题欢迎联系 hot_kun@hotmail.com
package auth

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"mini-ruoyi/internal/domain"
	"mini-ruoyi/internal/repository"
)

type sessionFixture struct {
	svc     *SessionService
	users   *repository.UserRepository
	session *repository.SessionRepository
	ctx     context.Context
}

func newSessionFixture(t *testing.T) *sessionFixture {
	t.Helper()

	db, err := repository.NewDB(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	ctx := context.Background()
	if err := repository.Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	users := repository.NewUserRepository(db)
	sessions := repository.NewSessionRepository(db)

	return &sessionFixture{
		svc:     NewSessionService(sessions, users),
		users:   users,
		session: sessions,
		ctx:     ctx,
	}
}

// TestSessionListNormalizesPaging 覆盖「会话列表绕过统一分页规范」这个缺陷。
//
// 原来是直接 (page-1)*pageSize 交给 SQL,没有 normalizePage / clampPage:
//   - page_size <= 0 会变成负数 LIMIT,SQLite 里等于取消限制,
//     接口会一次吐出全部会话
//   - page 越界时不钳位,响应里的 page 还是那个越界值,
//     前端分页器显示 "99 / 2" 这种不可能的状态
func TestSessionListNormalizesPaging(t *testing.T) {
	f := newSessionFixture(t)

	user, err := f.users.Create(f.ctx, domain.User{
		Status:   domain.StatusActive,
		Username: "sessionuser",
		Password: "x",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 造 5 条会话。ExpiresAt 必须设成未来——List 只返回未过期的。
	for i := 0; i < 5; i++ {
		if err := f.session.Create(f.ctx, domain.Session{
			TokenHash: "hash-" + string(rune('a'+i)),
			UserID:    user.ID,
			ExpiresAt: time.Now().Add(time.Hour),
			CSRFToken: "csrf",
			IP:        "192.0.2.1",
			UserAgent: "test",
		}); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("page_size 为 0 回落到默认值", func(t *testing.T) {
		got, err := f.svc.List(f.ctx, 1, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got.PageSize != 20 {
			t.Errorf("page_size = %d,期望回落到默认值 20", got.PageSize)
		}
	})

	t.Run("page_size 为负不能取消限制", func(t *testing.T) {
		got, err := f.svc.List(f.ctx, 1, -1)
		if err != nil {
			t.Fatal(err)
		}
		if got.PageSize != 20 {
			t.Errorf("page_size = %d,期望回落到默认值 20(负数 LIMIT 等于取消限制)", got.PageSize)
		}
	})

	t.Run("page_size 超上限钳到上限", func(t *testing.T) {
		got, err := f.svc.List(f.ctx, 1, 1000)
		if err != nil {
			t.Fatal(err)
		}
		if got.PageSize != 100 {
			t.Errorf("page_size = %d,期望钳到 100", got.PageSize)
		}
	})

	t.Run("page 越界钳到最后一页", func(t *testing.T) {
		got, err := f.svc.List(f.ctx, 99, 2)
		if err != nil {
			t.Fatal(err)
		}
		if got.Total != 5 {
			t.Fatalf("total = %d,期望 5", got.Total)
		}
		// 5 条 / 每页 2 → 最后一页是第 3 页
		if got.Page != 3 {
			t.Errorf("page = %d,期望钳到 3(否则前端显示 99 / 3)", got.Page)
		}
		if len(got.List) != 1 {
			t.Errorf("最后一页返回 %d 条,期望 1", len(got.List))
		}
	})

	t.Run("page 为 0 归一为第一页", func(t *testing.T) {
		got, err := f.svc.List(f.ctx, 0, 2)
		if err != nil {
			t.Fatal(err)
		}
		if got.Page != 1 {
			t.Errorf("page = %d,期望 1", got.Page)
		}
	})
}
