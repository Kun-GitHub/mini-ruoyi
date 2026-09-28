package service

import (
	"context"
	"path/filepath"

	"mini-ruoyi/internal/system"
)

// MonitorService 采集服务自身的运行状态。
type MonitorService struct {
	// dataDir 用于选择要观测的磁盘：数据库和上传文件都在这附近，
	// 盯根分区反而可能看错盘（数据盘常常单独挂载）
	dataDir string
}

func NewMonitorService(dataDir string) *MonitorService {
	return &MonitorService{dataDir: dataDir}
}

// Snapshot 返回一次运行状态快照。
//
// 没有走 repository：这些数据来自操作系统而不是数据库，
// 中间加一层仓储只是仪式感。
func (s *MonitorService) Snapshot(ctx context.Context) system.Snapshot {
	return system.Collect(ctx, s.dataDir)
}

// DataDir 返回被观测的目录，供启动日志展示。
func (s *MonitorService) DataDir() string {
	abs, err := filepath.Abs(s.dataDir)
	if err != nil {
		return s.dataDir
	}
	return abs
}
