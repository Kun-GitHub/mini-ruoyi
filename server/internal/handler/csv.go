package handler

import (
	"context"
	"fmt"
	"log"
	"mime"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/csvx"
	"mini-ruoyi/internal/httpx"
	"mini-ruoyi/internal/service"
)

// exportMaxRows 是单次导出的行数上限。
//
// 导出本身是流式的，内存与行数无关，但文件是给人打开的：十万行的 CSV 在
// Excel 里基本没法用。超过上限就让调用方先缩小筛选范围，比生成一个谁都
// 用不了的文件更诚实。
const exportMaxRows = 10000

// exportBatchSize 是流式导出每批取多少行。
// 服务端的 normalizePage 会把 pageSize 钳到同一量级，这里保持一致。
const exportBatchSize = 100

// streamCSV 把列表接口的**完整结果**（不只是当前页）写成 CSV 推给浏览器。
//
// 两件事按顺序做：先用第一页拿到 total，在写出任何字节之前判断行数上限。
// 一旦开始写 CSV 就没有回头路了——那时只能中途截断，而截断出来的文件
// 看起来是完整的，用户会以为数据只有这些。
func streamCSV[T any](
	c *gin.Context,
	filename string,
	header []string,
	fetch func(ctx context.Context, page, size int) (service.Page[T], error),
	row func(T) []string,
) {
	ctx := c.Request.Context()

	first, err := fetch(ctx, 1, exportBatchSize)
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	if first.Total > exportMaxRows {
		httpx.Fail(c, http.StatusBadRequest, httpx.KeyExportTooManyRows)
		return
	}

	startCSV(c, filename)
	w := csvx.NewWriter(c.Writer)
	if err := w.Write(header); err != nil {
		return
	}

	written := 0
	var writeErr error
	for page := 1; written < int(first.Total) && writeErr == nil; page++ {
		list := first.List
		if page > 1 {
			next, err := fetch(ctx, page, exportBatchSize)
			if err != nil {
				// 响应已经写出去了，无法再回错误；客户端会拿到半截文件，
				// 所以至少要留下日志，否则这次导出失败将是无声的。
				log.Printf("导出 %s 第 %d 页失败（响应已开始写出）: %v", filename, page, err)
				break
			}
			list = next.List
		}
		if len(list) == 0 {
			break
		}
		for _, item := range list {
			if writeErr = w.Write(row(item)); writeErr != nil {
				break // 多半是客户端断开，不必再往下取
			}
			written++
		}
	}

	w.Flush()
}

// startCSV 写导出响应的头。
//
// 文件名固定用 ASCII（users-20261008-143005.csv）：Content-Disposition 里
// 出现裸中文需要按 RFC 6266 编码，mime.FormatMediaType 会替我们做，
// 但 ASCII 文件名在任何客户端都不会出问题，没必要引入变数。
func startCSV(c *gin.Context, filename string) {
	c.Header("Content-Type", "text/csv; charset=utf-8")
	if d := mime.FormatMediaType("attachment", map[string]string{"filename": filename}); d != "" {
		c.Header("Content-Disposition", d)
	}
	// 与文件下载保持一致：浏览器不该猜内容类型
	c.Header("X-Content-Type-Options", "nosniff")
}

// csvFilename 生成带时间戳的文件名。
func csvFilename(prefix string, now time.Time) string {
	return fmt.Sprintf("%s-%s.csv", prefix, now.Format("20060102-150405"))
}

// csvTime 把时间写成 RFC3339（UTC）；零值输出空串。
//
// 不用 Excel 那种「2026/10/8 6:37」：它没有时区，导出的文件一转发就没人
// 说得清是哪个时区的时间。库里的时间本来就是 UTC，RFC3339 的 Z 自带说明。
func csvTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// csvTimePtr 同上，用于可空时间列（例如从未登录过）。
func csvTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return csvTime(*t)
}
