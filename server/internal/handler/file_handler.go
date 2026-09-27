package handler

import (
	"errors"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/domain"
	"mini-ruoyi/internal/httpx"
	"mini-ruoyi/internal/middleware"
	"mini-ruoyi/internal/repository"
	"mini-ruoyi/internal/service"
)

type FileHandler struct {
	svc *service.FileService
}

func NewFileHandler(svc *service.FileService) *FileHandler {
	return &FileHandler{svc: svc}
}

func (h *FileHandler) List(c *gin.Context) {
	page, pageSize := pageParams(c)

	result, err := h.svc.List(c.Request.Context(), repository.FileFilter{
		OriginalName: c.Query("name"),
		GroupName:    c.Query("group"),
	}, page, pageSize)
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, result)
}

// UploadMaxBytes 是上传接口自己的请求体上限。
//
// 全局 BodyLimit 是 1 MiB（普通 JSON 请求够用），上传必须放宽——
// 所以 router 里把 /files 前缀从全局限制中排除了，改由这里控制。
func (h *FileHandler) BodyLimit() int64 {
	// 多留 1 MiB 给 multipart 的边界与头部，否则正好卡在上限的文件会被误判为超大
	return h.svc.MaxSize() + (1 << 20)
}

func (h *FileHandler) Upload(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.BodyLimit())

	header, err := c.FormFile("file")
	if err != nil {
		// 请求体超限统一报「文件太大」。
		//
		// 不加这一步的话，超限会按「谁先发现」给出两种文案：
		// 文件大小在 (上限, 上限+1MiB) 之间由 service 拦下，报 error.fileTooLarge；
		// 再大一点就被 MaxBytesReader 拦下，报 error.bodyTooLarge。
		// 用户看到的都是「传了个大文件」，不该有两种说法。
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			httpx.FailFromError(c, domain.ErrFileTooLarge)
			return
		}
		httpx.FailBindError(c, err)
		return
	}

	src, err := header.Open()
	if err != nil {
		httpx.Fail(c, http.StatusBadRequest, httpx.KeyInvalidFile)
		return
	}
	defer src.Close()

	uploader, ok := middleware.CurrentUser(c)
	if !ok {
		httpx.Fail(c, http.StatusUnauthorized, httpx.KeyUnauthorized)
		return
	}

	created, err := h.svc.Upload(c.Request.Context(), service.UploadInput{
		OriginalName: filepath.Base(header.Filename), // 去掉客户端可能带的路径部分
		Size:         header.Size,
		ContentType:  header.Header.Get("Content-Type"),
		Group:        c.PostForm("group"),
		Uploader:     uploader,
		Content:      src,
	})
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, created)
}

// Download 流式返回文件。
//
// **一律强制下载**（Content-Disposition: attachment）：
// 上传一个 .html 再让它内联渲染，就等于在自己的域上执行别人的脚本。
// 配合 nosniff 与固定的 octet-stream，浏览器不会去猜类型。
//
// 用 http.ServeContent 而不是自己读文件再写：
//   - 它按块流式传输，不会把整个文件读进内存（1G 的机器上一个大文件就爆了）
//   - 白送 Range 支持（断点续传、下载器分片）
func (h *FileHandler) Download(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		return
	}

	f, handle, err := h.svc.GetForDownload(c.Request.Context(), id)
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	defer handle.Close()

	info, err := handle.Stat()
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}

	// mime.FormatMediaType 会按 RFC 5987 编码非 ASCII 文件名，
	// 中文文件名才不会变成乱码或触发 header 注入
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": f.OriginalName})
	if disposition == "" {
		disposition = "attachment"
	}

	c.Header("Content-Disposition", disposition)
	c.Header("Content-Type", "application/octet-stream")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Length", strconv.FormatInt(info.Size(), 10))

	http.ServeContent(c.Writer, c.Request, f.OriginalName, info.ModTime(), handle)
}

func (h *FileHandler) Delete(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, gin.H{"id": id})
}
