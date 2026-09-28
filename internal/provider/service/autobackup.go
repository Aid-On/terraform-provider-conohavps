// 自動バックアップの有効化・無効化と、自動バックアップで取得されたバックアップの一覧の API の呼び出しを提供する.
// ConoHa 独自の API のため、gophercloud のパッケージを使わずにリクエストを組み立てる.

package service

import (
	"context"
	"fmt"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// 自動バックアップの有効化オプションの構造体.
type EnableAutoBackupOpts struct {
	InstanceID string `json:"instance_uuid"`
	Schedule   string `json:"schedule"`
	Retention  int    `json:"retention"`
}

// 自動バックアップの有効化のレスポンス.
type AutoBackup struct {
	InstanceID string `json:"instance_uuid"`
	ID         string `json:"id"`
}

// 自動バックアップで取得されたバックアップ.
type Backup struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Status        string            `json:"status"`
	Size          int               `json:"size"`
	VolumeID      string            `json:"volume_id"`
	CreatedAt     string            `json:"created_at"`
	DataTimestamp string            `json:"data_timestamp"`
	Metadata      map[string]string `json:"metadata"`
}

// サーバーにアタッチされているボリュームの自動バックアップを有効にする.
// 追加ストレージのボリュームがアタッチされていれば、それもバックアップ対象になる.
func (c *ConohaClient) EnableAutoBackup(ctx context.Context, opts EnableAutoBackupOpts) (*AutoBackup, error) {
	if c.BlockStorageClient == nil {
		return nil, fmt.Errorf("block storage client is not initialized")
	}

	tflog.Debug(ctx, "Enabling auto-backup.", map[string]any{
		"instance_id": opts.InstanceID,
		"schedule":    opts.Schedule,
		"retention":   opts.Retention,
	})

	var out struct {
		Backup AutoBackup `json:"backup"`
	}
	_, err := c.BlockStorageClient.Post(ctx, c.BlockStorageClient.ServiceURL("backups"),
		map[string]any{"backup": opts}, &out, &gophercloud.RequestOpts{OkCodes: []int{201}})
	if err != nil {
		tflog.Error(ctx, "Auto-backup enabling error.", map[string]any{"error": err.Error()})
		return nil, fmt.Errorf("failed to enable auto-backup: %w", err)
	}

	tflog.Debug(ctx, "Finished enabling auto-backup.", map[string]any{
		"instance_id": out.Backup.InstanceID,
		"id":          out.Backup.ID,
	})

	return &out.Backup, nil
}

// サーバーにアタッチされているボリュームの自動バックアップを無効にする.
// サーバーが既に無い場合は成功として扱う.
func (c *ConohaClient) DisableAutoBackup(ctx context.Context, instanceID string) error {
	if c.BlockStorageClient == nil {
		return fmt.Errorf("block storage client is not initialized")
	}

	tflog.Debug(ctx, "Disabling auto-backup.", map[string]any{"instance_id": instanceID})

	_, err := c.BlockStorageClient.Delete(ctx, c.BlockStorageClient.ServiceURL("backups", instanceID),
		&gophercloud.RequestOpts{OkCodes: []int{204}})
	if err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			return nil
		}
		tflog.Error(ctx, "Auto-backup disabling error.", map[string]any{"error": err.Error()})
		return fmt.Errorf("failed to disable auto-backup: %w", err)
	}

	tflog.Debug(ctx, "Finished disabling auto-backup.", map[string]any{"instance_id": instanceID})

	return nil
}

// 自動バックアップで取得されたバックアップの詳細を一覧で取得する.
func (c *ConohaClient) ListBackups(ctx context.Context) ([]Backup, error) {
	if c.BlockStorageClient == nil {
		return nil, fmt.Errorf("block storage client is not initialized")
	}

	tflog.Debug(ctx, "Listing backups.", map[string]any{})

	var out struct {
		Backups []Backup `json:"backups"`
	}
	_, err := c.BlockStorageClient.Get(ctx, c.BlockStorageClient.ServiceURL("backups", "detail"),
		&out, &gophercloud.RequestOpts{OkCodes: []int{200}})
	if err != nil {
		return nil, fmt.Errorf("failed to list backups: %w", err)
	}

	return out.Backups, nil
}
