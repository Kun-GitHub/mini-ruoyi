package service

import (
	"context"
	"fmt"

	"gin-sqlite-example/internal/repository"
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

func (s *DeviceService) Create(ctx context.Context, in CreateDeviceInput) (repository.Device, error) {
	// 业务规则放这一层，比如：同名设备校验、默认值处理等
	return s.repo.Create(ctx, repository.Device{
		Name:     in.Name,
		Location: in.Location,
		Enabled:  in.Enabled,
	})
}

func (s *DeviceService) List(ctx context.Context, page, pageSize int) ([]repository.Device, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize
	return s.repo.List(ctx, pageSize, offset)
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
