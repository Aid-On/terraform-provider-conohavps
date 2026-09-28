// オブジェクトストレージ（OpenStack Swift 互換）の API を提供する.
// ConoHa のドキュメントに書かれたリクエスト（ヘッダーだけで設定を渡し、本文を持たない）をそのまま送るため、
// gophercloud の objectstorage パッケージは使わず、ServiceClient で直接リクエストを組み立てる.

package service

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// オブジェクトストレージの契約容量の単位（GB）.
const (
	// ObjectStorageQuotaStepGB は契約容量を指定できる単位.
	ObjectStorageQuotaStepGB int64 = 100
	// ObjectStorageQuotaDefaultGB は未契約時（既定）の契約容量.
	ObjectStorageQuotaDefaultGB int64 = 0
)

// コンテナの詳細取得（HEAD）で読める情報.
type ObjectStorageContainer struct {
	Name             string // コンテナ名
	ObjectCount      int64  // オブジェクト数（x-container-object-count）
	BytesUsed        int64  // 使用量（x-container-bytes-used）
	StoragePolicy    string // ストレージポリシー（x-storage-policy）
	VersionsLocation string // 古いオブジェクトの保存先コンテナ（x-versions-location）. 未設定なら空
	ContainerRead    string // Read 権限の ACL（x-container-read）. 未設定なら空
}

// アカウントの詳細取得（HEAD）で読める情報.
type ObjectStorageAccount struct {
	QuotaGB        int64 // 契約容量（GB）
	BytesUsed      int64 // 使用量（x-account-bytes-used）
	ContainerCount int64 // コンテナ数（x-account-container-count）
	ObjectCount    int64 // オブジェクト数（x-account-object-count）
}

// カタログにオブジェクトストレージが無ければエラーを返す.
func (c *ConohaClient) objectStorage() (*gophercloud.ServiceClient, error) {
	if c.ObjectStorageClient == nil {
		return nil, ServiceUnavailable("object-store")
	}
	return c.ObjectStorageClient, nil
}

// アカウントの URL（/v1/AUTH_{tenantid}）.
func accountURL(client *gophercloud.ServiceClient) string {
	return strings.TrimSuffix(client.Endpoint, "/")
}

// コンテナの URL（/v1/AUTH_{tenantid}/{container}）.
func containerURL(client *gophercloud.ServiceClient, name string) string {
	return accountURL(client) + "/" + url.PathEscape(name)
}

// CreateContainer コンテナを作成する（PUT /v1/AUTH_{tenantid}/{container}）.
// Swift は既存のコンテナへの PUT も成功（202）として扱い、黙って既存のコンテナを管理下に置いてしまうため、
// 先に存在を確かめ、あればエラーにする.
func (c *ConohaClient) CreateContainer(ctx context.Context, name string) error {
	client, err := c.objectStorage()
	if err != nil {
		return err
	}

	if _, err := c.GetContainer(ctx, name); err == nil {
		return fmt.Errorf("the container %q already exists; import it instead of creating it", name)
	} else if !gophercloud.ResponseCodeIs(err, http.StatusNotFound) {
		return err
	}

	tflog.Debug(ctx, "Creating container.", map[string]any{"name": name})
	_, err = client.Request(ctx, http.MethodPut, containerURL(client, name), &gophercloud.RequestOpts{
		OkCodes: []int{http.StatusCreated},
	})
	if err != nil {
		tflog.Error(ctx, "Container creation error.", map[string]any{"error": err.Error(), "name": name})
		return fmt.Errorf("failed to create container: %w", err)
	}
	return nil
}

// GetContainer コンテナの詳細を取得する（HEAD /v1/AUTH_{tenantid}/{container}）.
// コンテナが無ければ 404 のエラーを返す.
func (c *ConohaClient) GetContainer(ctx context.Context, name string) (*ObjectStorageContainer, error) {
	client, err := c.objectStorage()
	if err != nil {
		return nil, err
	}

	tflog.Debug(ctx, "Getting container.", map[string]any{"name": name})
	resp, err := client.Request(ctx, http.MethodHead, containerURL(client, name), &gophercloud.RequestOpts{
		OkCodes: []int{http.StatusOK, http.StatusNoContent},
	})
	if err != nil {
		return nil, err
	}

	h := resp.Header
	container := &ObjectStorageContainer{
		Name:             name,
		StoragePolicy:    h.Get("X-Storage-Policy"),
		VersionsLocation: h.Get("X-Versions-Location"),
		ContainerRead:    h.Get("X-Container-Read"),
	}
	if container.ObjectCount, err = headerInt(h, "X-Container-Object-Count"); err != nil {
		return nil, err
	}
	if container.BytesUsed, err = headerInt(h, "X-Container-Bytes-Used"); err != nil {
		return nil, err
	}
	return container, nil
}

// UpdateContainer コンテナの設定をヘッダーで変更する（POST /v1/AUTH_{tenantid}/{container}）.
// バージョニングと Web 公開の設定・解除はどちらもこの形で、値が空のヘッダーで解除する.
func (c *ConohaClient) UpdateContainer(ctx context.Context, name string, headers map[string]string) error {
	client, err := c.objectStorage()
	if err != nil {
		return err
	}

	tflog.Debug(ctx, "Updating container.", map[string]any{"name": name, "headers": headers})
	_, err = client.Request(ctx, http.MethodPost, containerURL(client, name), &gophercloud.RequestOpts{
		MoreHeaders: headers,
		OkCodes:     []int{http.StatusNoContent},
	})
	if err != nil {
		tflog.Error(ctx, "Container update error.", map[string]any{"error": err.Error(), "name": name})
		return fmt.Errorf("failed to update container: %w", err)
	}
	return nil
}

// DeleteContainer コンテナを削除する（DELETE /v1/AUTH_{tenantid}/{container}）.
// オブジェクトが残っているコンテナは API が削除を拒否し、そのエラーを返す.
func (c *ConohaClient) DeleteContainer(ctx context.Context, name string) error {
	client, err := c.objectStorage()
	if err != nil {
		return err
	}

	tflog.Debug(ctx, "Deleting container.", map[string]any{"name": name})
	_, err = client.Request(ctx, http.MethodDelete, containerURL(client, name), &gophercloud.RequestOpts{
		OkCodes: []int{http.StatusNoContent},
	})
	if err != nil {
		tflog.Error(ctx, "Container deletion error.", map[string]any{"error": err.Error(), "name": name})
		return err
	}
	return nil
}

// GetObjectStorageAccount アカウントの情報を取得する（HEAD /v1/AUTH_{tenantid}）.
func (c *ConohaClient) GetObjectStorageAccount(ctx context.Context) (*ObjectStorageAccount, error) {
	client, err := c.objectStorage()
	if err != nil {
		return nil, err
	}

	tflog.Debug(ctx, "Getting object storage account.", map[string]any{})
	resp, err := client.Request(ctx, http.MethodHead, accountURL(client), &gophercloud.RequestOpts{
		OkCodes: []int{http.StatusOK, http.StatusNoContent},
	})
	if err != nil {
		tflog.Error(ctx, "Object storage account read error.", map[string]any{"error": err.Error()})
		return nil, fmt.Errorf("failed to get object storage account: %w", err)
	}

	h := resp.Header
	account := &ObjectStorageAccount{}
	if account.QuotaGB, err = quotaGigaBytes(h); err != nil {
		return nil, err
	}
	if account.BytesUsed, err = headerInt(h, "X-Account-Bytes-Used"); err != nil {
		return nil, err
	}
	if account.ContainerCount, err = headerInt(h, "X-Account-Container-Count"); err != nil {
		return nil, err
	}
	if account.ObjectCount, err = headerInt(h, "X-Account-Object-Count"); err != nil {
		return nil, err
	}
	return account, nil
}

// SetObjectStorageQuota アカウントの契約容量を GB で設定する（POST /v1/AUTH_{tenantid}）.
// 使用量を下回る容量は API が拒否し、そのエラーを返す（黙って丸めたりしない）.
func (c *ConohaClient) SetObjectStorageQuota(ctx context.Context, gb int64) error {
	client, err := c.objectStorage()
	if err != nil {
		return err
	}
	if gb < 0 || gb%ObjectStorageQuotaStepGB != 0 {
		return fmt.Errorf("the object storage quota must be a multiple of %d GB, got %d", ObjectStorageQuotaStepGB, gb)
	}

	tflog.Debug(ctx, "Setting object storage quota.", map[string]any{"quota_gb": gb})
	_, err = client.Request(ctx, http.MethodPost, accountURL(client), &gophercloud.RequestOpts{
		MoreHeaders: map[string]string{"X-Account-Meta-Quota-Giga-Bytes": strconv.FormatInt(gb, 10)},
		OkCodes:     []int{http.StatusNoContent},
	})
	if err != nil {
		tflog.Error(ctx, "Object storage quota update error.", map[string]any{"error": err.Error(), "quota_gb": gb})
		return fmt.Errorf("failed to set object storage quota to %d GB: %w", gb, err)
	}
	return nil
}

// 契約容量を GB で読む.
// 設定したヘッダー（x-account-meta-quota-giga-bytes）が返ればそれを使い、
// 無ければドキュメントの例にあるバイト数（x-account-meta-quota-bytes）から換算する.
// バイト数の GB が 1000^3 か 1024^3 かはドキュメントに無いため、割り切れる方を採り、
// どちらでも割り切れなければ推測せずエラーにする.
func quotaGigaBytes(h http.Header) (int64, error) {
	if h.Get("X-Account-Meta-Quota-Giga-Bytes") != "" {
		return headerInt(h, "X-Account-Meta-Quota-Giga-Bytes")
	}
	b, err := headerInt(h, "X-Account-Meta-Quota-Bytes")
	if err != nil {
		return 0, err
	}
	const gib, gb = int64(1) << 30, int64(1_000_000_000)
	switch {
	case b%gib == 0:
		return b / gib, nil
	case b%gb == 0:
		return b / gb, nil
	default:
		return 0, fmt.Errorf("the object storage quota of %d bytes is not a whole number of GB", b)
	}
}

// 整数のヘッダーを読む. 無ければ 0 とする.
func headerInt(h http.Header, key string) (int64, error) {
	v := h.Get(key)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse the %s header %q: %w", key, v, err)
	}
	return n, nil
}
