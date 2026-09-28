// オブジェクトストレージ（OpenStack Swift 互換）の API を提供する.
// ConoHa のドキュメントに書かれたリクエスト（ヘッダーだけで設定を渡し、本文を持たない）をそのまま送るため、
// gophercloud の objectstorage パッケージは使わず、ServiceClient で直接リクエストを組み立てる.

package service

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
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

// コンテナに設定するヘッダー（OpenAPI 仕様の PUT・POST /v1/AUTH_{project_id}/{container_name}）.
// 解除は、値を空にする代わりに X-Remove- を前に付けたヘッダーで行う（X-Remove-Versions-Location と同じ形）.
const (
	HeaderVersionsLocation = "X-Versions-Location"               // 古いオブジェクトの保存先コンテナ
	HeaderContainerRead    = "X-Container-Read"                  // Read 権限の ACL
	HeaderContainerWrite   = "X-Container-Write"                 // Write 権限の ACL
	HeaderWebIndex         = "X-Container-Meta-Web-Index"        // Web 公開のインデックスファイル
	HeaderWebListings      = "X-Container-Meta-Web-Listings"     // Web 公開でオブジェクト一覧を表示するか
	HeaderWebListingsCSS   = "X-Container-Meta-Web-Listings-CSS" // オブジェクト一覧のスタイルシート
	HeaderWebError         = "X-Container-Meta-Web-Error"        // Web 公開のエラーファイルの接尾辞
	// ContainerMetaPrefix は任意のメタデータのヘッダーの接頭辞.
	ContainerMetaPrefix = "X-Container-Meta-"
)

// ReservedContainerMetaKeys は、任意のメタデータとしては扱わない X-Container-Meta- のキー（小文字）.
// Web 公開の設定は専用の属性で持つ. 一時 URL の鍵は秘密の値なので、メタデータとして State に写さない.
var ReservedContainerMetaKeys = []string{"web-index", "web-listings", "web-listings-css", "web-error", "temp-url-key", "temp-url-key-2"}

// IsReservedContainerMetaKey は、キーが任意のメタデータに使えない予約済みのものかを返す.
func IsReservedContainerMetaKey(key string) bool {
	return slices.Contains(ReservedContainerMetaKeys, strings.ToLower(key))
}

// RemoveHeader は、ヘッダー name を解除するヘッダー名（X-Remove-...）を返す.
func RemoveHeader(name string) string {
	return "X-Remove-" + strings.TrimPrefix(name, "X-")
}

// RemoveHeaderValue は X-Remove- ヘッダーに送る値. API は値を見ないが、空のヘッダーは途中で落とされうるため空にしない.
const RemoveHeaderValue = "x"

// EncodeVersionsLocation は、保存先コンテナ名を X-Versions-Location に送る形にする.
// OpenAPI 仕様のとおり、UTF-8 の名前を URL エンコードする.
func EncodeVersionsLocation(name string) string {
	return url.PathEscape(name)
}

// コンテナの詳細取得（HEAD）で読める情報.
type ObjectStorageContainer struct {
	Name             string            // コンテナ名
	ObjectCount      int64             // オブジェクト数（x-container-object-count）
	BytesUsed        int64             // 使用量（x-container-bytes-used）
	StoragePolicy    string            // ストレージポリシー（x-storage-policy）
	VersionsLocation string            // 古いオブジェクトの保存先コンテナ（x-versions-location を URL デコードしたもの）. 未設定なら空
	ContainerRead    string            // Read 権限の ACL（x-container-read）. 未設定なら空
	ContainerWrite   string            // Write 権限の ACL（x-container-write）. 未設定なら空
	WebIndex         string            // インデックスファイル（x-container-meta-web-index）. 未設定なら空
	WebListings      *bool             // オブジェクト一覧の表示（x-container-meta-web-listings）. 未設定なら nil
	WebListingsCSS   string            // オブジェクト一覧のスタイルシート（x-container-meta-web-listings-css）. 未設定なら空
	WebError         string            // エラーファイルの接尾辞（x-container-meta-web-error）. 未設定なら空
	Metadata         map[string]string // 予約済みを除く x-container-meta-*（キーは小文字）
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
// versionsLocation が空でなければ、OpenAPI 仕様のとおり作成と同時にバージョニングを有効にする.
// API は既存のコンテナへの PUT も成功（202）として扱い、送ったヘッダーで既存のコンテナを書き換えてしまうため、
// 先に存在を確かめ、あればエラーにする. 確かめた後に作られていた場合（202）もエラーにする.
func (c *ConohaClient) CreateContainer(ctx context.Context, name, versionsLocation string) error {
	client, err := c.objectStorage()
	if err != nil {
		return err
	}

	if _, err := c.GetContainer(ctx, name); err == nil {
		return fmt.Errorf("the container %q already exists; import it instead of creating it", name)
	} else if !gophercloud.ResponseCodeIs(err, http.StatusNotFound) {
		return err
	}

	headers := map[string]string{}
	if versionsLocation != "" {
		headers[HeaderVersionsLocation] = EncodeVersionsLocation(versionsLocation)
	}

	tflog.Debug(ctx, "Creating container.", map[string]any{"name": name, "headers": headers})
	resp, err := client.Request(ctx, http.MethodPut, containerURL(client, name), &gophercloud.RequestOpts{
		MoreHeaders: headers,
		OkCodes:     []int{http.StatusCreated, http.StatusAccepted},
	})
	if err != nil {
		tflog.Error(ctx, "Container creation error.", map[string]any{"error": err.Error(), "name": name})
		return fmt.Errorf("failed to create container: %w", err)
	}
	if resp.StatusCode == http.StatusAccepted {
		return fmt.Errorf("the container %q was created by someone else while this one was being created; import it instead of creating it", name)
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

	return parseContainer(name, resp.Header)
}

// HEAD のヘッダーをコンテナの情報にする.
func parseContainer(name string, h http.Header) (container *ObjectStorageContainer, err error) {
	container = &ObjectStorageContainer{
		Name:           name,
		StoragePolicy:  h.Get("X-Storage-Policy"),
		ContainerRead:  h.Get(HeaderContainerRead),
		ContainerWrite: h.Get(HeaderContainerWrite),
		WebIndex:       h.Get(HeaderWebIndex),
		WebListingsCSS: h.Get(HeaderWebListingsCSS),
		WebError:       h.Get(HeaderWebError),
		Metadata:       map[string]string{},
	}
	// 保存先は URL エンコードして送った形のまま返るため、デコードしてコンテナ名に戻す
	if v := h.Get(HeaderVersionsLocation); v != "" {
		if container.VersionsLocation, err = url.PathUnescape(v); err != nil {
			return nil, fmt.Errorf("failed to decode the %s header %q: %w", HeaderVersionsLocation, v, err)
		}
	}
	if v := h.Values(HeaderWebListings); len(v) > 0 {
		listings := isTrue(v[0])
		container.WebListings = &listings
	}
	for key, values := range h {
		if !strings.HasPrefix(key, ContainerMetaPrefix) {
			continue
		}
		name := strings.ToLower(strings.TrimPrefix(key, ContainerMetaPrefix))
		if !IsReservedContainerMetaKey(name) {
			container.Metadata[name] = values[0]
		}
	}
	if container.ObjectCount, err = headerInt(h, "X-Container-Object-Count"); err != nil {
		return nil, err
	}
	if container.BytesUsed, err = headerInt(h, "X-Container-Bytes-Used"); err != nil {
		return nil, err
	}
	return container, nil
}

// Swift が真として扱う値か（staticweb の config_true_value と同じ）.
func isTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes", "on", "t", "y":
		return true
	}
	return false
}

// UpdateContainer コンテナの設定をヘッダーで変更する（POST /v1/AUTH_{tenantid}/{container}）.
// バージョニング・ACL・Web 公開・メタデータの設定と解除（X-Remove- のヘッダー）はどれもこの形で行う.
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
// オブジェクトが残っているコンテナは API が 409 で削除を拒否し、そのエラーを返す.
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

// SetObjectStorageQuota アカウントの契約容量を GB で設定する（POST /v1/AUTH_{tenantid}、X-Account-Meta-Quota-Giga-Bytes）.
// OpenAPI 仕様のとおり 100 の倍数だけを送る. 解除のヘッダー（X-Remove-Account-Meta-Quota-Giga-Bytes）は仕様で使えないため、
// 契約をやめるときも 0 を設定する. 使用量を下回る容量は API が拒否し、そのエラーを返す（黙って丸めたりしない）.
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
// OpenAPI 仕様は HEAD の応答に契約容量のヘッダーを挙げていないため、読み方は Swift のメタデータの返し方から決める.
// 設定したヘッダー（x-account-meta-quota-giga-bytes）が返ればそれを使い、無ければバイト数（x-account-meta-quota-bytes）から換算する.
// 仕様は x-account-meta-quota-bytes を直接設定できないとしており、GB の設定から API が作る値と考えられる.
// バイト数の GB が 1000^3 か 1024^3 かは仕様に無いため、割り切れる方を採り、どちらでも割り切れなければ推測せずエラーにする.
// どちらのヘッダーも無ければ、契約容量が設定されていない（0GB）ものとする.
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
