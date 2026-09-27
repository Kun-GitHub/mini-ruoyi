package service

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"mini-ruoyi/internal/domain"
	"mini-ruoyi/internal/repository"
)

const (
	// logBufferSize 是内存里能堆积的日志条数上限。
	// 满了就丢并计数，**绝不阻塞请求**：日志丢几条可以接受，
	// 请求因为日志而变慢或卡住不可以。
	logBufferSize = 1024
	// logBatchSize / logFlushInterval 决定落库节奏：攒够一批或到点就写。
	logBatchSize     = 64
	logFlushInterval = 2 * time.Second
	// logCleanupBatch 是每次清理删除的行数上限。
	// 到期的行可能很多，一条大 DELETE 会长时间持有写锁让所有请求排队——
	// 分批删，剩下的下次再删。
	logCleanupBatch = 2000
	logWriteTimeout = 10 * time.Second
)

// LogService 负责收集并批量落库操作日志与登录日志。
//
// **为什么不直接在中间件里写库**：SQLite 是单写者。每个请求都写一行日志，
// 就是每个请求都去抢一次写锁并付一次事务开销，所有请求会在写锁上排队。
// 攒批之后，N 个请求只付一次写事务的代价。
//
// 缓冲满了丢日志而不是阻塞：这是审计功能，不该拖慢主流程。
type LogService struct {
	repo          *repository.LogRepository
	retentionDays int

	loginCh chan domain.LoginLog
	operCh  chan domain.OperLog

	droppedLogin atomic.Int64
	droppedOper  atomic.Int64

	stopOnce sync.Once
	done     chan struct{}
	flushReq chan chan struct{}
	wg       sync.WaitGroup
}

func NewLogService(repo *repository.LogRepository, retentionDays int) *LogService {
	if retentionDays <= 0 {
		retentionDays = 30
	}
	return &LogService{
		repo:          repo,
		retentionDays: retentionDays,
		loginCh:       make(chan domain.LoginLog, logBufferSize),
		operCh:        make(chan domain.OperLog, logBufferSize),
		done:          make(chan struct{}),
		flushReq:      make(chan chan struct{}),
	}
}

// Start 启动后台落库。必须在处理请求之前调用。
func (s *LogService) Start() {
	s.wg.Add(1)
	go s.run()
}

// Stop 停止并落库剩余日志。应在关闭 HTTP 服务之后、关闭数据库之前调用。
//
// 不调它的话，缓冲里最后几秒的日志会丢——而那恰恰是最可能出问题的时间段。
func (s *LogService) Stop() {
	s.stopOnce.Do(func() { close(s.done) })
	s.wg.Wait()

	if n := s.droppedLogin.Load() + s.droppedOper.Load(); n > 0 {
		log.Printf("日志服务：因缓冲已满丢弃了 %d 条记录", n)
	}
}

// userAgentLimit 对应 sys_login_logs / sys_oper_logs 里 user_agent 的声明宽度。
// SQLite 不强制长度，这里截断是为了让数据整洁。
const userAgentLimit = 255

// Flush 立即把缓冲区落库并等待完成。
//
// 给测试用（否则每个断言都要等 2 秒的刷盘间隔），
// 也让「关闭前确保落库」这件事有个明确的同步点。
func (s *LogService) Flush(ctx context.Context) error {
	ack := make(chan struct{})
	select {
	case s.flushReq <- ack:
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return nil
	}
	select {
	case <-ack:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RecordLogin 记录一次登录尝试。满时丢弃，不阻塞。
func (s *LogService) RecordLogin(l domain.LoginLog) {
	l.UserAgent = truncateBytes(l.UserAgent, userAgentLimit)
	select {
	case s.loginCh <- l:
	default:
		s.droppedLogin.Add(1)
	}
}

// RecordOper 记录一次写操作。满时丢弃，不阻塞。
func (s *LogService) RecordOper(l domain.OperLog) {
	l.UserAgent = truncateBytes(l.UserAgent, userAgentLimit)
	select {
	case s.operCh <- l:
	default:
		s.droppedOper.Add(1)
	}
}

// LoginLogPage / OperLogPage 与其它列表接口结构一致，前端分页逻辑可复用。
type LoginLogPage = Page[domain.LoginLog]
type OperLogPage = Page[domain.OperLog]

func (s *LogService) ListLoginLogs(ctx context.Context, f repository.LoginLogFilter, page, pageSize int) (LoginLogPage, error) {
	page, pageSize = normalizePage(page, pageSize)

	total, err := s.repo.CountLoginLogs(ctx, f)
	if err != nil {
		return LoginLogPage{}, fmt.Errorf("count login logs: %w", err)
	}
	page, offset := clampPage(page, pageSize, total)
	list, err := s.repo.ListLoginLogs(ctx, f, pageSize, offset)
	if err != nil {
		return LoginLogPage{}, fmt.Errorf("list login logs: %w", err)
	}
	return LoginLogPage{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

func (s *LogService) ListOperLogs(ctx context.Context, f repository.OperLogFilter, page, pageSize int) (OperLogPage, error) {
	page, pageSize = normalizePage(page, pageSize)

	total, err := s.repo.CountOperLogs(ctx, f)
	if err != nil {
		return OperLogPage{}, fmt.Errorf("count oper logs: %w", err)
	}
	page, offset := clampPage(page, pageSize, total)
	list, err := s.repo.ListOperLogs(ctx, f, pageSize, offset)
	if err != nil {
		return OperLogPage{}, fmt.Errorf("list oper logs: %w", err)
	}
	return OperLogPage{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

// RetentionDays 供启动日志展示。
func (s *LogService) RetentionDays() int { return s.retentionDays }

func (s *LogService) run() {
	defer s.wg.Done()

	ticker := time.NewTicker(logFlushInterval)
	defer ticker.Stop()

	logins := make([]domain.LoginLog, 0, logBatchSize)
	opers := make([]domain.OperLog, 0, logBatchSize)

	flush := func() {
		if len(logins) > 0 {
			writeBatch("登录日志", logins, s.repo.InsertLoginLogs)
			logins = logins[:0]
		}
		if len(opers) > 0 {
			writeBatch("操作日志", opers, s.repo.InsertOperLogs)
			opers = opers[:0]
		}
	}

	for {
		select {
		case l := <-s.loginCh:
			logins = append(logins, l)
			if len(logins) >= logBatchSize {
				writeBatch("登录日志", logins, s.repo.InsertLoginLogs)
				logins = logins[:0]
			}
		case o := <-s.operCh:
			opers = append(opers, o)
			if len(opers) >= logBatchSize {
				writeBatch("操作日志", opers, s.repo.InsertOperLogs)
				opers = opers[:0]
			}
		case ack := <-s.flushReq:
			flush()
			close(ack)
		case <-ticker.C:
			flush()
		case <-s.done:
			// 关闭前把两个通道里排队的都收干净。两个 case 交替取，
			// 直到都为空——只 drain 一个通道会丢掉另一种日志。
			for {
				select {
				case l := <-s.loginCh:
					logins = append(logins, l)
					continue
				case o := <-s.operCh:
					opers = append(opers, o)
					continue
				default:
				}
				break
			}
			flush()
			return
		}
	}
}

// writeBatch 落库一批日志。失败只记日志：日志写不进去不该影响主流程，
// 但也不能静默——否则「审计记录缺失」这件事没人会知道。
//
// 包级函数而非方法：Go 不允许方法带类型参数。
func writeBatch[T any](name string, batch []T, insert func(context.Context, []T) error) {
	ctx, cancel := context.WithTimeout(context.Background(), logWriteTimeout)
	defer cancel()

	if err := insert(ctx, batch); err != nil {
		log.Printf("写入%s失败（%d 条）: %v", name, len(batch), err)
	}
}

// CleanupExpired 按保留期删除过期日志，返回删除条数。供定时任务调用。
//
// 一次只删一批：到期的行可能很多，一条大 DELETE 会长时间持有写锁
// 让所有请求排队。剩下的下次执行再删。
//
// 清理**不再由本服务自己起 ticker**：什么时候跑什么维护任务，
// 统一交给 internal/job，界面上看得见、能暂停、能手动触发。
func (s *LogService) CleanupExpired(ctx context.Context) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -s.retentionDays)
	return s.repo.DeleteLogsBefore(ctx, cutoff, logCleanupBatch)
}

// truncateBytes 按字节上限截断，且不切坏 UTF-8。
//
// 放在这一层而不是各个调用方：落库形状是这一层的职责，
// 中间件和 handler 都往这里投递，统一处理一次就够了。
func truncateBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
