// 自動バックアップの申し込み・保存期間の変更・解約と、自動バックアップで取得されたバックアップの一覧の API の呼び出しを提供する.
// ConoHa 独自の API のため、gophercloud のパッケージを使わずにリクエストを組み立てる.

package service

import (
	"context"
	"fmt"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// 自動バックアップの有効化オプションの構造体.
// スケジュール（schedule）は API 仕様で非推奨となり、指定できる値が既定値の daily だけなので送らない.
type EnableAutoBackupOpts struct {
	InstanceID string `json:"instance_uuid"`
	Retention  int    `json:"retention"`
}

// 自動バックアップの有効化のレスポンス.
type AutoBackup struct {
	InstanceID string `json:"instance_uuid"`
	ID         string `json:"id"`
}

// 自動バックアップで取得されたバックアップ.
// metadata（instance_uuid・is_boot_volume）は API 仕様のスキーマに無く、バックアップ詳細一覧のドキュメントの応答例にだけ載る.
type Backup struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	Status              string            `json:"status"`
	Size                int               `json:"size"`
	VolumeID            string            `json:"volume_id"`
	CreatedAt           string            `json:"created_at"`
	UpdatedAt           string            `json:"updated_at"`
	DataTimestamp       string            `json:"data_timestamp"`
	IsIncremental       bool              `json:"is_incremental"`
	HasDependentBackups bool              `json:"has_dependent_backups"`
	Metadata            map[string]string `json:"metadata"`
}

// サーバーにアタッチされているボリュームの自動バックアップを有効にする.
// 追加ストレージのボリュームがアタッチされていれば、それもバックアップ対象になる.
func (c *ConohaClient) EnableAutoBackup(ctx context.Context, opts EnableAutoBackupOpts) (*AutoBackup, error) {
	if c.BlockStorageClient == nil {
		return nil, fmt.Errorf("block storage client is not initialized")
	}

	tflog.Debug(ctx, "Enabling auto-backup.", map[string]any{
		"instance_id": opts.InstanceID,
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

// 日次バックアップの保存期間（日数）を変更し、変更後の保存期間を返す.
// 日次バックアップを申し込んでいるサーバーにだけ実行できる.
func (c *ConohaClient) UpdateAutoBackupRetention(ctx context.Context, instanceID string, retention int) (int, error) {
	if c.BlockStorageClient == nil {
		return 0, fmt.Errorf("block storage client is not initialized")
	}

	tflog.Debug(ctx, "Updating auto-backup retention.", map[string]any{
		"instance_id": instanceID,
		"retention":   retention,
	})

	var out struct {
		Backup struct {
			Retention int `json:"retention"`
		} `json:"backup"`
	}
	_, err := c.BlockStorageClient.Put(ctx, c.BlockStorageClient.ServiceURL("backups", instanceID),
		map[string]any{"backup": map[string]any{"retention": retention}}, &out, &gophercloud.RequestOpts{OkCodes: []int{200}})
	if err != nil {
		tflog.Error(ctx, "Auto-backup retention updating error.", map[string]any{"error": err.Error()})
		return 0, fmt.Errorf("failed to update auto-backup retention: %w", err)
	}

	tflog.Debug(ctx, "Finished updating auto-backup retention.", map[string]any{
		"instance_id": instanceID,
		"retention":   out.Backup.Retention,
	})

	return out.Backup.Retention, nil
}

// サーバーの自動バックアップを解約する（日次・週次の両方）. 取得済みのバックアップは削除されない.
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
