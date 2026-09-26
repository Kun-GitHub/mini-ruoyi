package repository

import (
	"context"
	"path/filepath"
	"testing"
)

func newTestRepo(t *testing.T) (*DeviceRepository, context.Context) {
	t.Helper()
	db, err := NewDB(filepath.Join(t.TempDir(), "devices.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return NewDeviceRepository(db), ctx
}

// TestCreateReturnsGeneratedFields 是 A3 的回归用例：
// created_at 由数据库 DEFAULT 生成，不 RETURNING 回来调用方只能拿到零值。
func TestCreateReturnsGeneratedFields(t *testing.T) {
	repo, ctx := newTestRepo(t)

	got, err := repo.Create(ctx, Device{Name: "sensor-1", Location: "lab", Enabled: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.ID == 0 {
		t.Error("Create 未回填 ID")
	}
	if got.CreatedAt.IsZero() {
		t.Error("Create 未回填 CreatedAt")
	}
}

// TestListReturnsNonNilSliceForEmptyResult 是 A4 的回归用例：
// 空结果必须是 [] 而不是 null，否则序列化后 data.list 为 null，前端要特判。
func TestListReturnsNonNilSliceForEmptyResult(t *testing.T) {
	repo, ctx := newTestRepo(t)

	list, err := repo.List(ctx, 20, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if list == nil {
		t.Error("List 返回 nil，期望空切片")
	}
	if len(list) != 0 {
		t.Errorf("空表返回了 %d 条记录", len(list))
	}
}

func TestCountAndPagination(t *testing.T) {
	repo, ctx := newTestRepo(t)

	for _, name := range []string{"a", "b", "c"} {
		if _, err := repo.Create(ctx, Device{Name: name, Location: "lab"}); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}

	total, err := repo.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if total != 3 {
		t.Errorf("Count = %d，期望 3", total)
	}

	// 按 id DESC 排序，取第 2 页（每页 2 条）应只剩 1 条
	page, err := repo.List(ctx, 2, 2)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page) != 1 {
		t.Fatalf("第 2 页返回 %d 条，期望 1", len(page))
	}
	if page[0].Name != "a" {
		t.Errorf("第 2 页首条是 %q，期望 a", page[0].Name)
	}
}

func TestUpdateAndDeleteNotFound(t *testing.T) {
	repo, ctx := newTestRepo(t)

	if err := repo.Update(ctx, 999, true); err != ErrNotFound {
		t.Errorf("Update 不存在的 id 返回 %v，期望 ErrNotFound", err)
	}
	if err := repo.Delete(ctx, 999); err != ErrNotFound {
		t.Errorf("Delete 不存在的 id 返回 %v，期望 ErrNotFound", err)
	}
	if _, err := repo.GetByID(ctx, 999); err != ErrNotFound {
		t.Errorf("GetByID 不存在的 id 返回 %v，期望 ErrNotFound", err)
	}
}
