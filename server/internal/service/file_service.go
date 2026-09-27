package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"mini-ruoyi/internal/domain"
	"mini-ruoyi/internal/repository"
)

// FileService 管理文件元数据与磁盘上的本体。
type FileService struct {
	repo    *repository.FileRepository
	dir     string // 绝对路径
	maxSize int64
	quota   int64
}

// NewFileService 准备上传目录。
//
// 目录在**启动时**创建并绝对化：配置写错（路径不存在、没写权限）应当让进程起不来，
// 而不是等到用户第一次上传才报错——那时错误现场已经离配置很远了。
func NewFileService(repo *repository.FileRepository, dir string, maxSize, quota int64) (*FileService, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("解析上传目录 %q: %w", dir, err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("创建上传目录 %q: %w", abs, err)
	}
	return &FileService{repo: repo, dir: abs, maxSize: maxSize, quota: quota}, nil
}

// Dir 返回上传目录的绝对路径，供启动日志展示。
func (s *FileService) Dir() string { return s.dir }

// UploadInput 是一次上传所需的全部输入。
//
// Content 由 handler 提供（multipart 的 file part），service 不感知 HTTP。
type UploadInput struct {
	OriginalName string
	Size         int64
	ContentType  string
	Group        string
	Uploader     domain.User
	Content      io.Reader
}

// Upload 落盘并登记。
//
// 顺序是**先落文件、再写库**：反过来会留下一堆指向空文件的记录，
// 用户点下载得到 404；而先落文件最坏只留下孤儿文件，由定时任务回收。
func (s *FileService) Upload(ctx context.Context, in UploadInput) (domain.File, error) {
	if in.Size <= 0 {
		return domain.File{}, fmt.Errorf("文件为空: %w", domain.ErrInvalidFile)
	}
	if in.Size > s.maxSize {
		return domain.File{}, fmt.Errorf("文件 %d 字节超过上限 %d: %w", in.Size, s.maxSize, domain.ErrFileTooLarge)
	}

	used, _, err := s.repo.Usage(ctx)
	if err != nil {
		return domain.File{}, fmt.Errorf("读取已用容量: %w", err)
	}
	if s.quota > 0 && used+in.Size > s.quota {
		return domain.File{}, fmt.Errorf("已用 %d + 本次 %d 超过配额 %d: %w",
			used, in.Size, s.quota, domain.ErrQuotaExceeded)
	}

	// 磁盘名一律由我们生成：用户给的名字直接当路径就是路径穿越
	rel, err := newStoragePath()
	if err != nil {
		return domain.File{}, fmt.Errorf("生成存储路径: %w", err)
	}
	abs := filepath.Join(s.dir, filepath.FromSlash(rel))

	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return domain.File{}, fmt.Errorf("创建目录: %w", err)
	}
	if err := writeFileAtomic(abs, in.Content); err != nil {
		return domain.File{}, fmt.Errorf("写入文件: %w", err)
	}

	created, err := s.repo.Create(ctx, domain.File{
		GroupName:    in.Group,
		OriginalName: in.OriginalName,
		StoragePath:  rel,
		Size:         in.Size,
		ContentType:  in.ContentType,
		UploaderID:   in.Uploader.ID,
		UploaderName: in.Uploader.Username,
	})
	if err != nil {
		// 库没写进去，磁盘上的文件就是孤儿——立刻删掉，别等清理任务
		if rmErr := os.Remove(abs); rmErr != nil {
			log.Printf("上传登记失败后清理文件 %s 也失败: %v", abs, rmErr)
		}
		return domain.File{}, fmt.Errorf("登记文件: %w", err)
	}
	return created, nil
}

// writeFileAtomic 先写临时文件再 rename。
//
// 直接写目标文件的话，中途失败会留下半个文件，而它的大小和内容是错的——
// rename 在同一文件系统内是原子的，要么完整可见，要么完全不存在。
func writeFileAtomic(abs string, r io.Reader) error {
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".upload-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		// 成功路径上 tmp 已经被 rename 掉，这里的删除会失败，忽略
		_ = os.Remove(tmpName)
	}()

	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, abs)
}

// FilePage 是文件列表。除了标准分页字段，还带上容量用量——
// 界面在同一次请求里就能显示「已用/配额」，不必再多一次往返。
type FilePage struct {
	List     []domain.File    `json:"list"`
	Total    int64            `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
	Usage    domain.FileUsage `json:"usage"`
}

func (s *FileService) List(ctx context.Context, f repository.FileFilter, page, pageSize int) (FilePage, error) {
	page, pageSize = normalizePage(page, pageSize)

	total, err := s.repo.Count(ctx, f)
	if err != nil {
		return FilePage{}, fmt.Errorf("count files: %w", err)
	}
	page, offset := clampPage(page, pageSize, total)

	list, err := s.repo.List(ctx, f, pageSize, offset)
	if err != nil {
		return FilePage{}, fmt.Errorf("list files: %w", err)
	}
	used, count, err := s.repo.Usage(ctx)
	if err != nil {
		return FilePage{}, fmt.Errorf("read usage: %w", err)
	}

	return FilePage{
		List: list, Total: total, Page: page, PageSize: pageSize,
		Usage: domain.FileUsage{Used: used, Quota: s.quota, Count: count, MaxSize: s.maxSize},
	}, nil
}

// GetForDownload 返回记录与打开的文件句柄，调用方负责 Close。
func (s *FileService) GetForDownload(ctx context.Context, id int64) (domain.File, *os.File, error) {
	f, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return domain.File{}, nil, err
	}
	handle, err := os.Open(filepath.Join(s.dir, filepath.FromSlash(f.StoragePath)))
	if err != nil {
		// 记录在、文件不在：不该发生（孤儿清理只删没有记录的文件），
		// 但真出现了要给一个能看懂的响应，而不是 500
		if os.IsNotExist(err) {
			return domain.File{}, nil, fmt.Errorf("文件 %d 的本体已丢失（%s）: %w", id, f.StoragePath, domain.ErrNotFound)
		}
		return domain.File{}, nil, fmt.Errorf("打开文件 %d: %w", id, err)
	}
	return f, handle, nil
}

// Delete 先删库记录、再删磁盘文件。
//
// 顺序与上传相反，是为了「宁可留孤儿，也不留指向空文件的记录」：
// 第二步失败只是浪费一点磁盘，由定时任务回收；
// 反过来则会让用户看到一条点开就 404 的记录。
func (s *FileService) Delete(ctx context.Context, id int64) error {
	f, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get file %d: %w", id, err)
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete file %d: %w", id, err)
	}

	abs := filepath.Join(s.dir, filepath.FromSlash(f.StoragePath))
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		// 记录已经删掉了，这里失败只留下孤儿文件，可以接受
		log.Printf("删除文件 %d 的本体失败（留下孤儿，稍后由定时任务回收）: %v", id, err)
	}
	return nil
}

// Usage 返回容量用量。
func (s *FileService) Usage(ctx context.Context) (domain.FileUsage, error) {
	used, count, err := s.repo.Usage(ctx)
	if err != nil {
		return domain.FileUsage{}, fmt.Errorf("read usage: %w", err)
	}
	return domain.FileUsage{Used: used, Quota: s.quota, Count: count, MaxSize: s.maxSize}, nil
}

// MaxSize 供界面提示与上传前的预检。
func (s *FileService) MaxSize() int64 { return s.maxSize }

// CleanupOrphans 删除磁盘上没有对应记录的文件，返回删除个数。供定时任务调用。
//
// 孤儿只可能来自两处：上传「先落文件后写库」时写库失败（那时会立即尝试删掉），
// 以及删除时「先删库后删文件」的第二步失败。两处都是「宁可留孤儿」的取舍结果。
func (s *FileService) CleanupOrphans(ctx context.Context) (int64, error) {
	known, err := s.repo.AllStoragePaths(ctx)
	if err != nil {
		return 0, fmt.Errorf("读取已知文件列表: %w", err)
	}

	var removed int64
	err = filepath.WalkDir(s.dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			// 单个条目读不了不该中断整轮清理
			log.Printf("遍历 %s 时出错: %v", path, walkErr)
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(s.dir, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		// 上传过程中的临时文件也一并收掉
		if strings.HasPrefix(d.Name(), ".upload-") {
			if err := os.Remove(path); err == nil {
				removed++
			}
			return nil
		}
		if _, ok := known[rel]; ok {
			return nil
		}
		if err := os.Remove(path); err != nil {
			log.Printf("删除孤儿文件 %s 失败: %v", rel, err)
			return nil
		}
		removed++
		return nil
	})
	if err != nil {
		return removed, fmt.Errorf("遍历上传目录: %w", err)
	}
	return removed, nil
}

// newStoragePath 生成 `<前两位>/<32位十六进制>` 形式的相对路径。
//
// 分两级目录：上万文件挤在一个目录里，某些文件系统上会明显退化。
// 用随机名而不是自增 id：自增名可以被顺序枚举，而文件名不该是个可猜的 URL。
func newStoragePath() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	name := hex.EncodeToString(buf)
	return name[:2] + "/" + name, nil
}
