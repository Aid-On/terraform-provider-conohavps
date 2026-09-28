// ボリュームのスナップショットの API の呼び出しを提供する.

package service

import (
	"context"
	"fmt"
	"slices"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/snapshots"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// スナップショットの作成オプションの構造体.
type CreateSnapshotOpts struct {
	VolumeID    string
	Name        string
	Description string
}

// スナップショットを作成し、available になるまで待つ.
// ConoHa ではスナップショットは1つのみ作成でき、24時間経過で自動削除される.
func (c *ConohaClient) CreateSnapshot(ctx context.Context, opts CreateSnapshotOpts) (*snapshots.Snapshot, error) {
	if c.BlockStorageClient == nil {
		return nil, fmt.Errorf("block storage client is not initialized")
	}

	tflog.Debug(ctx, "Creating snapshot.", map[string]any{
		"volume_id": opts.VolumeID,
		"name":      opts.Name,
	})

	snapshot, err := snapshots.Create(ctx, c.BlockStorageClient, snapshots.CreateOpts{
		VolumeID:    opts.VolumeID,
		Name:        opts.Name,
		Description: opts.Description,
	}).Extract()
	if err != nil {
		tflog.Error(ctx, "Snapshot creation error.", map[string]any{"error": err.Error()})
		return nil, fmt.Errorf("failed to create snapshot: %w", err)
	}

	snapshot, err = c.waitForSnapshotStatus(ctx, snapshot.ID, "available")
	if err != nil {
		return nil, fmt.Errorf("failed to wait for snapshot to be available: %w", err)
	}

	tflog.Debug(ctx, "Finished creating snapshot.", map[string]any{"id": snapshot.ID})

	return snapshot, nil
}

// スナップショットを取得する.
func (c *ConohaClient) GetSnapshot(ctx context.Context, id string) (*snapshots.Snapshot, error) {
	if c.BlockStorageClient == nil {
		return nil, fmt.Errorf("block storage client is not initialized")
	}

	tflog.Debug(ctx, "Getting snapshot.", map[string]any{"id": id})

	snapshot, err := snapshots.Get(ctx, c.BlockStorageClient, id).Extract()
	if err != nil {
		return nil, fmt.Errorf("failed to get snapshot: %w", err)
	}

	return snapshot, nil
}

// スナップショットを削除し、消えるまで待つ.
// 削除できるのはステータスが available または error のときだけなので、そのどちらかになるまで待ってから削除する.
// 既に無い場合は成功として扱う.
func (c *ConohaClient) DeleteSnapshot(ctx context.Context, id string) error {
	if c.BlockStorageClient == nil {
		return fmt.Errorf("block storage client is not initialized")
	}

	tflog.Debug(ctx, "Deleting snapshot.", map[string]any{"id": id})

	if _, err := c.waitForSnapshotStatus(ctx, id, "available", "error"); err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			return nil
		}
		return fmt.Errorf("failed to wait for snapshot to be deletable: %w", err)
	}

	if err := snapshots.Delete(ctx, c.BlockStorageClient, id).ExtractErr(); err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			return nil
		}
		tflog.Error(ctx, "Snapshot deletion error.", map[string]any{"error": err.Error()})
		return fmt.Errorf("failed to delete snapshot: %w", err)
	}

	err := gophercloud.WaitFor(ctx, func(ctx context.Context) (bool, error) {
		_, err := c.GetSnapshot(ctx, id)
		if gophercloud.ResponseCodeIs(err, 404) {
			return true, nil
		}
		return false, err
	})
	if err != nil {
		return fmt.Errorf("failed to wait for snapshot deletion: %w", err)
	}

	tflog.Debug(ctx, "Finished deleting snapshot.", map[string]any{"id": id})

	return nil
}

// スナップショットが statuses のいずれかになるまで待つ.
// statuses に error を含めない限り、error になった時点で失敗とする.
func (c *ConohaClient) waitForSnapshotStatus(ctx context.Context, id string, statuses ...string) (*snapshots.Snapshot, error) {
	var current *snapshots.Snapshot
	err := gophercloud.WaitFor(ctx, func(ctx context.Context) (bool, error) {
		snapshot, err := c.GetSnapshot(ctx, id)
		if err != nil {
			return false, err
		}
		current = snapshot
		if slices.Contains(statuses, snapshot.Status) {
			return true, nil
		}
		if snapshot.Status == "error" {
			return false, fmt.Errorf("snapshot %s is in error state", id)
		}
		return false, nil
	})
	return current, err
}
