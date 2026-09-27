package job

import (
	"context"
	"log"
)

// 依赖用接口声明在**使用方**这一侧（与项目其它地方一致）：
// 这样 job 包不必 import service / auth，也就不会有循环依赖。
type (
	// SessionCleaner 清理过期会话，返回清理条数。
	SessionCleaner interface {
		CleanupExpired(ctx context.Context) (int64, error)
	}
	// LogCleaner 清理超过保留期的日志，返回清理条数。
	LogCleaner interface {
		CleanupExpired(ctx context.Context) (int64, error)
		RetentionDays() int
	}
	// OrphanFileCleaner 清理磁盘上没有对应记录的文件，返回清理条数。
	OrphanFileCleaner interface {
		CleanupOrphans(ctx context.Context) (int64, error)
	}
)

// 任务 key。命名约定 `动作:对象`，便于按前缀分组与排查。
const (
	KeyCleanupExpiredSessions = "cleanup:expired_sessions"
	KeyCleanupOldLogs         = "cleanup:old_logs"
	KeyCleanupOrphanFiles     = "cleanup:orphan_files"
)

// CleanupExpiredSessions 每小时清理一次过期会话。
//
// 登录时也会顺手清一次（白捡的），但那只在有人登录时才发生——
// 长期无人登录的部署需要这个兜底。
func CleanupExpiredSessions(sessions SessionCleaner) Job {
	return Job{
		Key:            KeyCleanupExpiredSessions,
		DescriptionKey: "job.cleanupExpiredSessions",
		DefaultCron:    "0 * * * *",
		Run: func(ctx context.Context) error {
			n, err := sessions.CleanupExpired(ctx)
			if err != nil {
				return err
			}
			if n > 0 {
				log.Printf("清理了 %d 条过期会话", n)
			}
			return nil
		},
	}
}

// CleanupOldLogs 每天凌晨清理超过保留期的日志。
//
// 放在凌晨而不是正午：这条语句会持有写锁，挑一个没人用的时间点。
func CleanupOldLogs(logs LogCleaner) Job {
	return Job{
		Key:            KeyCleanupOldLogs,
		DescriptionKey: "job.cleanupOldLogs",
		DefaultCron:    "30 4 * * *",
		Run: func(ctx context.Context) error {
			n, err := logs.CleanupExpired(ctx)
			if err != nil {
				return err
			}
			if n > 0 {
				log.Printf("清理了 %d 条超过 %d 天的日志", n, logs.RetentionDays())
			}
			return nil
		},
	}
}

// CleanupOrphanFiles 每天清理磁盘上没有对应数据库记录的文件。
//
// 孤儿是这么产生的：删除时「先删库、再删磁盘」，第二步失败就留下了文件。
// 反过来「先删磁盘再删库」更糟——数据库里会留下指向空文件的记录，
// 用户点下载得到 404。所以宁可留孤儿，再定期收。
func CleanupOrphanFiles(files OrphanFileCleaner) Job {
	return Job{
		Key:            KeyCleanupOrphanFiles,
		DescriptionKey: "job.cleanupOrphanFiles",
		DefaultCron:    "0 5 * * *",
		Run: func(ctx context.Context) error {
			n, err := files.CleanupOrphans(ctx)
			if err != nil {
				return err
			}
			if n > 0 {
				log.Printf("清理了 %d 个孤儿文件", n)
			}
			return nil
		},
	}
}
