// 有问题欢迎联系 hot_kun@hotmail.com
package service

import (
	"net/http"
	"testing"

	"mini-ruoyi/internal/domain"
	"mini-ruoyi/internal/repository"
)

// TestFlushWritesRowsStillQueued 钉住 Flush 的契约。
//
// 「调用返回时，调用之前记下的日志已经落库」——不成立的话刷盘之后立刻查
// 就会偶发少一条（`httpserver` 里那条断言就是这样随机红的）。
//
// 原因在 run() 的 select：通道里有待收的行、同时有人要求刷盘时，
// select 是**随机**挑一个 case 的。挑中 flushReq 就只写了本地那批，
// 排队的行留在通道里，而 Flush 的 ack 已经关了。
//
// 所以这里每轮都「记一条立刻刷一次，刷完就数」，不等到最后一起数：
// 中间那一次漏掉只是把行推到下一轮，只有最后一轮才看得出来。
func TestFlushWritesRowsStillQueued(t *testing.T) {
	f := newJobFixture(t)

	svc := NewLogService(repository.NewLogRepository(f.db), 30)
	svc.Start()
	t.Cleanup(svc.Stop)

	const rounds = 20
	for i := 0; i < rounds; i++ {
		svc.RecordOper(domain.OperLog{
			Username: "flusher",
			Method:   http.MethodPost,
			Path:     "/api/v1/users",
			Status:   http.StatusOK,
		})
		if err := svc.Flush(f.ctx); err != nil {
			t.Fatalf("第 %d 轮 Flush: %v", i, err)
		}

		page, err := svc.ListOperLogs(f.ctx, repository.OperLogFilter{Username: "flusher"}, 1, 1)
		if err != nil {
			t.Fatalf("ListOperLogs: %v", err)
		}
		if page.Total != int64(i+1) {
			t.Fatalf("第 %d 轮 Flush 返回后库里 %d 条，期望 %d 条——排队的行没被刷进去",
				i, page.Total, i+1)
		}
	}
}
