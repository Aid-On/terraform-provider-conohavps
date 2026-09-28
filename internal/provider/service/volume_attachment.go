// サーバーへのボリュームのアタッチ・デタッチの API の呼び出しを提供する.

package service

import (
	"context"
	"fmt"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/volumeattach"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// サーバーにボリュームをアタッチし、ボリュームが in-use になるまで待つ.
// ConoHa ではサーバーが停止している必要があり、追加ストレージ用のボリュームは1つまでしかアタッチできない.
// これらの制約の判定は API に任せる.
func (c *ConohaClient) AttachVolume(ctx context.Context, instanceID, volumeID string) (*volumeattach.VolumeAttachment, error) {
	if c.ComputeClient == nil || c.BlockStorageClient == nil {
		return nil, fmt.Errorf("compute or block storage client is not initialized")
	}

	tflog.Debug(ctx, "Attaching volume.", map[string]any{
		"instance_id": instanceID,
		"volume_id":   volumeID,
	})

	attachment, err := volumeattach.Create(ctx, c.ComputeClient, instanceID, volumeattach.CreateOpts{
		VolumeID: volumeID,
	}).Extract()
	if err != nil {
		tflog.Error(ctx, "Volume attach error.", map[string]any{"error": err.Error()})
		return nil, fmt.Errorf("failed to attach volume: %w", err)
	}

	if err := NewBlockStorageService(c.BlockStorageClient).WaitForVolumeStatus(ctx, volumeID, "in-use"); err != nil {
		return nil, fmt.Errorf("failed to wait for volume to be in-use: %w", err)
	}

	tflog.Debug(ctx, "Finished attaching volume.", map[string]any{
		"instance_id": instanceID,
		"volume_id":   volumeID,
		"device":      attachment.Device,
	})

	return attachment, nil
}

// サーバーにアタッチされているボリュームを取得する.
func (c *ConohaClient) GetVolumeAttachment(ctx context.Context, instanceID, volumeID string) (*volumeattach.VolumeAttachment, error) {
	if c.ComputeClient == nil {
		return nil, fmt.Errorf("compute client is not initialized")
	}

	tflog.Debug(ctx, "Getting volume attachment.", map[string]any{
		"instance_id": instanceID,
		"volume_id":   volumeID,
	})

	attachment, err := volumeattach.Get(ctx, c.ComputeClient, instanceID, volumeID).Extract()
	if err != nil {
		return nil, fmt.Errorf("failed to get volume attachment: %w", err)
	}

	return attachment, nil
}

// サーバーからボリュームをデタッチし、ボリュームが available になるまで待つ.
// アタッチが既に無い場合は成功として扱う.
func (c *ConohaClient) DetachVolume(ctx context.Context, instanceID, volumeID string) error {
	if c.ComputeClient == nil || c.BlockStorageClient == nil {
		return fmt.Errorf("compute or block storage client is not initialized")
	}

	tflog.Debug(ctx, "Detaching volume.", map[string]any{
		"instance_id": instanceID,
		"volume_id":   volumeID,
	})

	if err := volumeattach.Delete(ctx, c.ComputeClient, instanceID, volumeID).ExtractErr(); err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			return nil
		}
		tflog.Error(ctx, "Volume detach error.", map[string]any{"error": err.Error()})
		return fmt.Errorf("failed to detach volume: %w", err)
	}

	err := NewBlockStorageService(c.BlockStorageClient).WaitForVolumeStatus(ctx, volumeID, "available")
	if err != nil && !gophercloud.ResponseCodeIs(err, 404) {
		return fmt.Errorf("failed to wait for volume to be available: %w", err)
	}

	tflog.Debug(ctx, "Finished detaching volume.", map[string]any{
		"instance_id": instanceID,
		"volume_id":   volumeID,
	})

	return nil
}
