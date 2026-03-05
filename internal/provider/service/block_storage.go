// ボリューム関連のAPIの呼び出しを提供する.

package service

import (
	"context"
	"fmt"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type BlockStorageService struct {
	client *gophercloud.ServiceClient
}

func NewBlockStorageService(client *gophercloud.ServiceClient) *BlockStorageService {
	return &BlockStorageService{
		client: client,
	}
}

// 作成オプションの構造体.
type CreateVolumeOpts struct {
	Size        int
	Description string
	Name        string
	VolumeType  string
	ImageID     string
	SourceVolID string
	BackupID    string
}

// ボリュームの作成.
func (s *BlockStorageService) CreateVolume(ctx context.Context, opts CreateVolumeOpts) (*volumes.Volume, error) {
	createOpts := volumes.CreateOpts{
		Size:        opts.Size,
		Description: opts.Description,
		Name:        opts.Name,
		VolumeType:  opts.VolumeType,
		ImageID:     opts.ImageID,
		SourceVolID: opts.SourceVolID,
		BackupID:    opts.BackupID,
	}

	tflog.Debug(ctx, "Creating volume.", map[string]any{
		"name":        opts.Name,
		"size":        opts.Size,
		"volume_type": opts.VolumeType,
		"image_id":    opts.ImageID,
		"source_vol":  opts.SourceVolID,
		"backup_id":   opts.BackupID,
	})

	volume, err := volumes.Create(ctx, s.client, createOpts, nil).Extract()
	if err != nil {
		return nil, fmt.Errorf("failed to create volume: %w", err)
	}

	// ボリュームが利用可能になるまで待機
	if err := s.WaitForVolumeStatus(ctx, volume.ID, "available"); err != nil {
		return nil, fmt.Errorf("failed to wait for volume to be available: %w", err)
	}

	// 最新の状態を取得するためにボリュームを再取得
	volume, err = s.GetVolume(ctx, volume.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get volume after creation: %w", err)
	}

	tflog.Debug(ctx, "Finished creating volume.", map[string]any{
		"id": volume.ID,
	})

	return volume, nil
}

// ボリュームの取得.
func (s *BlockStorageService) GetVolume(ctx context.Context, id string) (*volumes.Volume, error) {
	tflog.Debug(ctx, "Getting volume.", map[string]any{
		"id": id,
	})

	volume, err := volumes.Get(ctx, s.client, id).Extract()
	if err != nil {
		return nil, fmt.Errorf("failed to get volume: %w", err)
	}

	tflog.Debug(ctx, "Finished getting volume.", map[string]any{
		"id": id,
	})

	return volume, nil
}

type UpdateVolumeOpts struct {
	Name        string
	Description string
}

// ボリュームの更新.
func (s *BlockStorageService) UpdateVolume(ctx context.Context, id string, opts UpdateVolumeOpts) (*volumes.Volume, error) {
	updateOpts := volumes.UpdateOpts{
		Name:        &opts.Name,
		Description: &opts.Description,
	}

	tflog.Debug(ctx, "Updating volume.", map[string]any{
		"id":          id,
		"name":        opts.Name,
		"description": opts.Description,
	})

	volume, err := volumes.Update(ctx, s.client, id, updateOpts).Extract()
	if err != nil {
		return nil, fmt.Errorf("failed to update volume: %w", err)
	}

	tflog.Debug(ctx, "Finished updating volume.", map[string]any{
		"id": id,
	})

	return volume, nil
}

// ボリュームの削除.
func (s *BlockStorageService) DeleteVolume(ctx context.Context, id string) error {
	tflog.Debug(ctx, "Deleting volume.", map[string]any{
		"id": id,
	})

	err := volumes.Delete(ctx, s.client, id, volumes.DeleteOpts{}).ExtractErr()
	if err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			return nil
		}
		return fmt.Errorf("failed to delete volume: %w", err)
	}

	// ボリュームが削除されるまで待機
	if err := s.waitForVolumeDelete(ctx, id); err != nil {
		return fmt.Errorf("failed to wait for volume deletion: %w", err)
	}

	tflog.Debug(ctx, "Finished deleting volume.", map[string]any{
		"id": id,
	})

	return nil
}

// ボリュームが指定されたステータスになるまで待機.
func (s *BlockStorageService) WaitForVolumeStatus(ctx context.Context, id, status string) error {
	tflog.Debug(ctx, "Waiting for volume status.", map[string]any{
		"id":     id,
		"status": status,
	})

	return gophercloud.WaitFor(ctx, func(ctx context.Context) (bool, error) {
		current, err := s.GetVolume(ctx, id)
		if err != nil {
			return false, err
		}

		if current.Status == "error" {
			return false, fmt.Errorf("volume %s is in error state", id)
		}

		if current.Status == status {
			return true, nil
		}

		return false, nil
	})
}

// ボリュームが削除されるまで待機.
func (s *BlockStorageService) waitForVolumeDelete(ctx context.Context, id string) error {
	tflog.Debug(ctx, "Waiting for volume deletion.", map[string]any{
		"id": id,
	})

	return gophercloud.WaitFor(ctx, func(ctx context.Context) (bool, error) {
		_, err := s.GetVolume(ctx, id)
		if err != nil {
			if gophercloud.ResponseCodeIs(err, 404) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
}
