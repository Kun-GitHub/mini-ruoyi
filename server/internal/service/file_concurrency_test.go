// 有问题欢迎联系 hot_kun@hotmail.com
package service

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mini-ruoyi/internal/domain"
	"mini-ruoyi/internal/repository"
)

type fileFixture struct {
	svc     *FileService
	repo    *repository.FileRepository
	dir     string
	ctx     context.Context
	cleanup func()
}

func newFileFixture(t *testing.T, maxSize, quota int64) *fileFixture {
	t.Helper()

	db, err := repository.NewDB(filepath.Join(t.TempDir(), "files.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	ctx := context.Background()
	if err := repository.Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	dir := t.TempDir()
	repo := repository.NewFileRepository(db)
	svc, err := NewFileService(repo, dir, maxSize, quota)
	if err != nil {
		t.Fatalf("NewFileService: %v", err)
	}

	return &fileFixture{svc: svc, repo: repo, dir: svc.Dir(), ctx: ctx}
}

func uploadN(t *testing.T, f *fileFixture, name string, size int) (domain.File, error) {
	t.Helper()
	return f.svc.Upload(f.ctx, UploadInput{
		OriginalName: name,
		Size:         int64(size),
		ContentType:  "text/plain",
		Group:        "test",
		Uploader:     domain.User{ID: 1, Username: "tester"},
		Content:      bytes.NewReader(bytes.Repeat([]byte("x"), size)),
	})
}

// TestUploadRejectsOverQuota 确认配额本身有效——并发那条测试只有在这个前提下才有意义。
func TestUploadRejectsOverQuota(t *testing.T) {
	f := newFileFixture(t, 1024, 100)

	if _, err := uploadN(t, f, "a.txt", 60); err != nil {
		t.Fatalf("第一次上传应成功: %v", err)
	}
	if _, err := uploadN(t, f, "b.txt", 60); !errors.Is(err, domain.ErrQuotaExceeded) {
		t.Errorf("超过配额应返回 ErrQuotaExceeded,实际: %v", err)
	}
}

// TestUploadQuotaSurvivesConcurrency 验证并发上传后总量不超配额。
//
// 要防的缺陷是「先读总量、再写入」的两步流程:两个并发上传读到同一个 used,
// 各自都算得下,一起写进去就超了。修法是把判断搬进 INSERT 的 WHERE,
// 由 SQLite 的单写者模型替我们串行化。
//
// 需要说明的是:这个用例**不能可靠复现**原来的竞态。
// 实测把实现改回两步版本、并发提到 40 个写者并用 WaitGroup 对齐起点,
// 它依然通过——磁盘 IO 发生在配额检查之后,靠前的写者往往在后面的还没读到
// used 时就已完成提交。它能证伪的是实现引入的其他错误:总量超过配额、
// 或者落库条数与成功次数对不上。
//
// 原子性本身由 SQL 结构保证,不靠这个用例:配额判断和插入是同一条语句,
// 中间没有可交错的位置。
func TestUploadQuotaSurvivesConcurrency(t *testing.T) {
	f := newFileFixture(t, 1024, 100)

	const attempts = 40
	var wg sync.WaitGroup
	var start sync.WaitGroup
	start.Add(1)
	results := make([]error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			start.Wait() // 尽量让所有写者同时开始，放大交错窗口
			_, err := uploadN(t, f, "race.txt", 60)
			results[idx] = err
		}(i)
	}
	start.Done()
	wg.Wait()

	succeeded := 0
	for _, err := range results {
		if err == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Errorf("配额 100、每个 60 字节:应恰好成功 1 次,实际 %d 次", succeeded)
	}

	used, count, err := f.repo.Usage(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if used > 100 {
		t.Errorf("已用 %d 字节,超过配额 100", used)
	}
	if count != int64(succeeded) {
		t.Errorf("落库 %d 条,与成功次数 %d 不一致", count, succeeded)
	}
}

// TestCleanupOrphansKeepsFreshFiles 覆盖「清理任务删掉正在上传的文件」这个竞态。
//
// 上传是「先落盘、后写库」,两步之间文件在库里确实不存在。
// 清理任务恰好在这时扫过就会把它删掉,而上传完成后写进库的记录
// 指向一个不存在的文件——用户看到上传成功、下载 404。
func TestCleanupOrphansKeepsFreshFiles(t *testing.T) {
	f := newFileFixture(t, 1024, 0)

	// 直接往上传目录里放一个文件,模拟「已落盘、还没登记」的中间态
	fresh := filepath.Join(f.dir, "aa", "in-flight")
	if err := os.MkdirAll(filepath.Dir(fresh), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(fresh, bytes.NewReader([]byte("正在上传"))); err != nil {
		t.Fatal(err)
	}

	removed, err := f.svc.CleanupOrphans(f.ctx)
	if err != nil {
		t.Fatalf("CleanupOrphans: %v", err)
	}
	if removed != 0 {
		t.Errorf("删除了 %d 个文件,期望 0——正在上传的文件不该被当孤儿", removed)
	}
	if !exists(t, fresh) {
		t.Error("刚落盘、还没登记的文件被清理任务删掉了")
	}
}

// TestCleanupOrphansRemovesOldFiles 确认冷静期不是「什么都不删」:
// 过了冷静期的真孤儿仍然要被收走,否则这个任务就没用了。
func TestCleanupOrphansRemovesOldFiles(t *testing.T) {
	f := newFileFixture(t, 1024, 0)

	old := filepath.Join(f.dir, "bb", "old-orphan")
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(old, bytes.NewReader([]byte("孤儿"))); err != nil {
		t.Fatal(err)
	}
	// 把修改时间推回到冷静期之外
	past := time.Now().Add(-2 * orphanGracePeriod)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}

	removed, err := f.svc.CleanupOrphans(f.ctx)
	if err != nil {
		t.Fatalf("CleanupOrphans: %v", err)
	}
	if removed != 1 {
		t.Errorf("删除了 %d 个,期望 1", removed)
	}
	if exists(t, old) {
		t.Error("过期的孤儿文件应被删除")
	}
}

// TestCleanupOrphansKeepsRegisteredFiles 确认已登记的文件无论如何都不动,
// 即使它的修改时间已经很老。
func TestCleanupOrphansKeepsRegisteredFiles(t *testing.T) {
	f := newFileFixture(t, 1024, 0)

	created, err := uploadN(t, f, "keep.txt", 10)
	if err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(f.dir, filepath.FromSlash(created.StoragePath))
	past := time.Now().Add(-2 * orphanGracePeriod)
	if err := os.Chtimes(abs, past, past); err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.CleanupOrphans(f.ctx); err != nil {
		t.Fatalf("CleanupOrphans: %v", err)
	}
	if !exists(t, abs) {
		t.Error("已登记的文件被清理任务删掉了")
	}
}

// TestCleanupOrphansKeepsTempFilesOfInFlightUploads 确认 .upload- 临时文件
// 也在冷静期内受保护:它是 writeFileAtomic 正在写的中间态,
// 删掉它会让那次上传直接失败。
func TestCleanupOrphansKeepsTempFilesOfInFlightUploads(t *testing.T) {
	f := newFileFixture(t, 1024, 0)

	tmp := filepath.Join(f.dir, ".upload-abc123")
	if err := os.WriteFile(tmp, []byte("半截"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.CleanupOrphans(f.ctx); err != nil {
		t.Fatalf("CleanupOrphans: %v", err)
	}
	if !exists(t, tmp) {
		t.Error("正在写入的临时文件被清理任务删掉了")
	}

	// 过了冷静期的残留临时文件仍然要收
	stale := filepath.Join(f.dir, ".upload-stale")
	if err := os.WriteFile(stale, []byte("残留"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-2 * orphanGracePeriod)
	if err := os.Chtimes(stale, past, past); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CleanupOrphans(f.ctx); err != nil {
		t.Fatal(err)
	}
	if exists(t, stale) {
		t.Error("过期的临时文件应被删除")
	}
}

// TestRemoveIfStillOrphanRestoresRegisteredFile 覆盖「查库」与「删除」之间的那个窗口。
//
// 场景：清理任务已经判定某路径是孤儿，但在它真正动手之前，
// 一个上传完成了 writeFileAtomic 并把记录写进库。
// 修法是先 rename 到隔离名再复核，发现已登记就把文件放回原名——
// 这一步能验证“放回”确实发生，而单纯 os.Remove 做不到。
//
// 另外这里也确认复核之后目录里不留下 .trash- 残留。
func TestRemoveIfStillOrphanRestoresRegisteredFile(t *testing.T) {
	f := newFileFixture(t, 1024, 0)

	created, err := uploadN(t, f, "later.txt", 10)
	if err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(f.dir, filepath.FromSlash(created.StoragePath))

	removed, err := f.svc.removeIfStillOrphan(f.ctx, abs, created.StoragePath)
	if err != nil {
		t.Fatalf("removeIfStillOrphan: %v", err)
	}
	if removed {
		t.Error("已登记的文件被当成孤儿删掉了：用户会看到上传成功但下载 404")
	}
	if !exists(t, abs) {
		t.Error("复核发现已登记后，文件没有被放回原名")
	}

	// 目录里不能留下隔离名的残留文件
	entries, err := os.ReadDir(filepath.Dir(abs))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".trash-") {
			t.Errorf("留下隔离残留文件 %s", e.Name())
		}
	}
}

// TestUploadRejectsEmptyFile 守住 size<=0 这条既有约束,顺带确认改动没把它弄丢。
func TestUploadRejectsEmptyFile(t *testing.T) {
	f := newFileFixture(t, 1024, 0)
	if _, err := uploadN(t, f, "empty.txt", 0); !errors.Is(err, domain.ErrInvalidFile) {
		t.Errorf("空文件应返回 ErrInvalidFile,实际: %v", err)
	}
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	return err == nil
}
