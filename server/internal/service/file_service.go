// 有问题欢迎联系 hot_kun@hotmail.com
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
//
// 配额判断和登记合成一条 SQL（见 CreateWithinQuota），不再先读一遍总量：
// 两步之间并发进来的另一个上传会读到同一个 used，两个都以为装得下。
func (s *FileService) Upload(ctx context.Context, in UploadInput) (domain.File, error) {
	if in.Size <= 0 {
		return domain.File{}, fmt.Errorf("文件为空: %w", domain.ErrInvalidFile)
	}
	if in.Size > s.maxSize {
		return domain.File{}, fmt.Errorf("文件 %d 字节超过上限 %d: %w", in.Size, s.maxSize, domain.ErrFileTooLarge)
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

	created, err := s.repo.CreateWithinQuota(ctx, domain.File{
		GroupName:    in.Group,
		OriginalName: in.OriginalName,
		StoragePath:  rel,
		Size:         in.Size,
		ContentType:  in.ContentType,
		UploaderID:   in.Uploader.ID,
		UploaderName: in.Uploader.Username,
	}, s.quota)
	if err != nil {
		// 登记失败（含超配额），磁盘上的文件就是孤儿——立刻删掉，别等清理任务。
		// 配额拒绝也走这里：文件已经落盘了，不能留在那儿占地方。
		if rmErr := os.Remove(abs); rmErr != nil {
			log.Printf("上传登记失败后清理文件 %s 也失败: %v", abs, rmErr)
		}
		if errors.Is(err, domain.ErrQuotaExceeded) {
			return domain.File{}, fmt.Errorf("超过配额 %d: %w", s.quota, domain.ErrQuotaExceeded)
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

// orphanGracePeriod 是孤儿文件的「冷静期」。
//
// 上传是「先落盘、后写库」，两步之间那个文件在库里确实不存在。
// 如果清理任务恰好在这时扫过，它会把一个正在上传的文件删掉，
// 而上传完成后写进库的记录就指向一个不存在的文件——用户看到上传成功、下载 404。
//
// 用时间阈值而不是锁：清理任务与上传分属不同请求、不同进程的可能性都存在，
// 进程内的锁盖不住。一个文件落盘超过这个时长还没登记，就真的没人会来登记了。
const orphanGracePeriod = time.Hour

// CleanupOrphans 删除磁盘上没有对应记录的文件，返回删除个数。供定时任务调用。
//
// 孤儿只可能来自两处：上传「先落文件后写库」时写库失败（那时会立即尝试删掉），
// 以及删除时「先删库后删文件」的第二步失败。两处都是「宁可留孤儿」的取舍结果。
func (s *FileService) CleanupOrphans(ctx context.Context) (int64, error) {
	// 先取磁盘清单、再查库：这个顺序下，查库之后才登记的文件不会被漏判成孤儿。
	// 因为它在库里存在，而库里存在就不会被删。
	known, err := s.repo.StoragePathsCreatedBefore(ctx, time.Now().Add(-orphanGracePeriod))
	if err != nil {
		return 0, fmt.Errorf("读取已知文件列表: %w", err)
	}
	// 冷静期内的文件不参与判定，但也不该被删——单独记一份全集用于排除
	recent, err := s.repo.AllStoragePaths(ctx)
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

		// 上传过程中的临时文件也一并收掉。
		//
		// 同样要过冷静期：`.upload-` 是 writeFileAtomic 正在写的中间态，
		// 另一个请求上传时，这个临时文件正在被写，删掉它会让那次上传直接失败。
		if strings.HasPrefix(d.Name(), ".upload-") {
			if info, statErr := d.Info(); statErr == nil && time.Since(info.ModTime()) < orphanGracePeriod {
				return nil
			}
			if err := os.Remove(path); err == nil {
				removed++
			}
			return nil
		}
		if _, ok := recent[rel]; ok {
			// 库里已经知道这个文件（无论何时创建的），无论如何不动它
			return nil
		}
		if _, ok := known[rel]; ok {
			return nil
		}

		// 落盘时间也在冷静期内：可能是正在上传，也可能只是个新建的空文件，先放过
		if info, statErr := d.Info(); statErr == nil && time.Since(info.ModTime()) < orphanGracePeriod {
			return nil
		}

		// 删除前最后一次确认：从拿到清单到轮到这个文件，中间隔了整个磁盘遍历的时间，
		// 足够一个上传把自己的登记写完。
		removedNow, err := s.removeIfStillOrphan(ctx, path, rel)
		if err != nil {
			log.Printf("处理 %s 时出错，本轮跳过: %v", rel, err)
			return nil
		}
		if removedNow {
			removed++
		}
		return nil
	})
	if err != nil {
		return removed, fmt.Errorf("遍历上传目录: %w", err)
	}
	return removed, nil
}

// removeIfStillOrphan 删除一个疑似孤儿文件，并消除「复核」与「删除」之间的窗口。
//
// 单纯「查一次库 → os.Remove」是不够的：检查完之后、删之前的那一刻，
// 另一个请求完全可能完成 writeFileAtomic 并把记录写进库，
// 于是文件被删而记录还在——用户看到上传成功、下载 404。
// 复核只把窗口变窄，没有关掉它。
//
// 所以先 rename 再复核：rename 到隔离名之后，原来的路径已经空出。
// 此时如果上传刚把记录写好，说明我们判错了，把文件 rename 回原名即可，
// 文件与记录重新一致。若复核仍确认未登记，就真的删掉。
// rename 与后续删除都在同一目录内，不会跨文件系统。
func (s *FileService) removeIfStillOrphan(ctx context.Context, path, rel string) (bool, error) {
	tmp := path + ".trash-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if err := os.Rename(path, tmp); err != nil {
		if os.IsNotExist(err) {
			return false, nil // 已经不在了，不是错误
		}
		return false, err
	}

	registered, err := s.repo.ExistsStoragePath(ctx, rel)
	if err != nil {
		// 查不到就当成已登记，把它放回去——宁可漏删一个孤儿文件，
		// 也不能凭一次失败的查询删掉别人的正在上传的文件。
		if restoreErr := os.Rename(tmp, path); restoreErr != nil {
			log.Printf("恢复 %s 失败（文件现停在 %s）: %v", rel, tmp, restoreErr)
		}
		return false, err
	}
	if registered {
		// 上传在我们 rename 之后完成了登记，把文件放回原名
		if restoreErr := os.Rename(tmp, path); restoreErr != nil {
			log.Printf("恢复 %s 失败（文件现停在 %s）: %v", rel, tmp, restoreErr)
			return false, restoreErr
		}
		return false, nil
	}

	if err := os.Remove(tmp); err != nil {
		return false, err
	}
	return true, nil
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
