package service

import (
	"context"
	"fmt"

	"mini-ruoyi/internal/repository"
)

type DeviceService struct {
	repo *repository.DeviceRepository
}

func NewDeviceService(repo *repository.DeviceRepository) *DeviceService {
	return &DeviceService{repo: repo}
}

type CreateDeviceInput struct {
	Name     string
	Location string
	Enabled  bool
}

// DevicePage 是列表接口的统一分页结构。list 永远是非 nil 数组。
type DevicePage struct {
	List     []repository.Device `json:"list"`
	Total    int64               `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
}

func (s *DeviceService) Create(ctx context.Context, in CreateDeviceInput) (repository.Device, error) {
	// 业务规则放这一层，比如：同名设备校验、默认值处理等
	return s.repo.Create(ctx, repository.Device{
		Name:     in.Name,
		Location: in.Location,
		Enabled:  in.Enabled,
	})
}

func (s *DeviceService) List(ctx context.Context, page, pageSize int) (DevicePage, error) {
	page, pageSize = normalizePage(page, pageSize)

	total, err := s.repo.Count(ctx)
	if err != nil {
		return DevicePage{}, fmt.Errorf("count devices: %w", err)
	}
	list, err := s.repo.List(ctx, pageSize, (page-1)*pageSize)
	if err != nil {
		return DevicePage{}, fmt.Errorf("list devices: %w", err)
	}
	return DevicePage{
		List:     list,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func (s *DeviceService) Get(ctx context.Context, id int64) (repository.Device, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *DeviceService) SetEnabled(ctx context.Context, id int64, enabled bool) error {
	if err := s.repo.Update(ctx, id, enabled); err != nil {
		return fmt.Errorf("update device %d: %w", id, err)
	}
	return nil
}

func (s *DeviceService) Delete(ctx context.Context, id int64) error {
	return s.repo.Delete(ctx, id)
}

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}
