package domain

import "time"

// 任务执行结果的取值。空串表示从未执行过。
const (
	JobStatusNever   = ""
	JobStatusSuccess = "success"
	JobStatusFailed  = "failed"
	// JobStatusSkipped 表示这一次因为「上一次还没跑完」被跳过。
	// 单独一个状态是有必要的：它解释「为什么今天没跑」。
	JobStatusSkipped = "skipped"
)

// Job 是定时任务的**可调部分**。任务本体在代码里（internal/job）。
//
// 界面上不提供「新建任务」：造出来的任务永远不会被执行，
// 因为没有任何代码会去跑它。这与权限点不能在界面上新建是同一个道理。
type Job struct {
	ID        int64     `json:"-"`
	CreatedAt time.Time `json:"-"`
	UpdatedAt time.Time `json:"-"`
	Status    string    `json:"status"`
	Key       string    `json:"job_key"`
	Cron      string    `json:"cron"`
	Remark    string    `json:"remark"`

	// 以下来自代码注册表，不落库
	DescriptionKey string `json:"description_key"`
	DefaultCron    string `json:"default_cron"`

	// 最近一次执行
	LastRunAt      *time.Time `json:"last_run_at"`
	LastStatus     string     `json:"last_status"`
	LastError      string     `json:"last_error"`
	LastDurationMS int        `json:"last_duration_ms"`

	// NextRunAt 由调度器算出，不落库
	NextRunAt *time.Time `json:"next_run_at"`
}

func (j *Job) IsActive() bool { return j.Status == StatusActive }
