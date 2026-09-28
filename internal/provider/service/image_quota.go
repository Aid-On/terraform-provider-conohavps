// イメージ保存容量（ConoHa 独自の /v2/quota）と、その使用量の API を提供する.

package service

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// イメージ保存容量の単位（GB）.
const (
	// ImageQuotaDefaultGB は既定（無料）のイメージ保存容量. これより小さくは設定できない.
	ImageQuotaDefaultGB int64 = 50
	// ImageQuotaStepGB は既定の容量に加えられる単位.
	ImageQuotaStepGB int64 = 500
)

// イメージ保存容量のリクエストとレスポンスの本文.
type imageQuotaBody struct {
	Quota struct {
		ImageSize string `json:"image_size"` // "550GB" のように単位付きの文字列
	} `json:"quota"`
}

// イメージ保存使用量のレスポンスの本文.
type imageUsageBody struct {
	Images struct {
		Size int64 `json:"size"` // 使用量（byte）
	} `json:"images"`
}

// イメージのクライアントを返す.
func (c *ConohaClient) imageService() (*gophercloud.ServiceClient, error) {
	if c.ImageClient == nil {
		return nil, fmt.Errorf("image client is not initialized")
	}
	if c.ImageClient.ProviderClient == nil && c.ProviderClient != nil {
		c.ImageClient.ProviderClient = c.ProviderClient
	}
	return c.ImageClient, nil
}

// GetImageQuota イメージ保存容量を GB で取得する（GET /v2/quota）.
func (c *ConohaClient) GetImageQuota(ctx context.Context) (int64, error) {
	client, err := c.imageService()
	if err != nil {
		return 0, err
	}

	tflog.Debug(ctx, "Getting image quota.", map[string]any{})
	var body imageQuotaBody
	_, err = client.Get(ctx, client.ServiceURL("quota"), &body, &gophercloud.RequestOpts{
		OkCodes: []int{http.StatusOK},
	})
	if err != nil {
		tflog.Error(ctx, "Image quota read error.", map[string]any{"error": err.Error()})
		return 0, fmt.Errorf("failed to get image quota: %w", err)
	}
	return ParseImageSize(body.Quota.ImageSize)
}

// SetImageQuota イメージ保存容量を GB で変更する（PUT /v2/quota）. 変更後の容量を返す.
// 使用量を下回る容量は API が拒否し、そのエラーを返す.
func (c *ConohaClient) SetImageQuota(ctx context.Context, gb int64) (int64, error) {
	client, err := c.imageService()
	if err != nil {
		return 0, err
	}
	if !ValidImageQuota(gb) {
		return 0, fmt.Errorf("the image quota must be %d GB plus a multiple of %d GB, got %d", ImageQuotaDefaultGB, ImageQuotaStepGB, gb)
	}

	var req, resp imageQuotaBody
	req.Quota.ImageSize = FormatImageSize(gb)

	tflog.Debug(ctx, "Setting image quota.", map[string]any{"image_size": req.Quota.ImageSize})
	_, err = client.Put(ctx, client.ServiceURL("quota"), req, &resp, &gophercloud.RequestOpts{
		OkCodes: []int{http.StatusOK},
	})
	if err != nil {
		tflog.Error(ctx, "Image quota update error.", map[string]any{"error": err.Error(), "image_size": req.Quota.ImageSize})
		return 0, fmt.Errorf("failed to set image quota to %s: %w", req.Quota.ImageSize, err)
	}
	return ParseImageSize(resp.Quota.ImageSize)
}

// GetImageUsage イメージ保存容量の使用量を byte で取得する（GET /v2/images/total）.
func (c *ConohaClient) GetImageUsage(ctx context.Context) (int64, error) {
	client, err := c.imageService()
	if err != nil {
		return 0, err
	}

	tflog.Debug(ctx, "Getting image usage.", map[string]any{})
	var body imageUsageBody
	_, err = client.Get(ctx, client.ServiceURL("images", "total"), &body, &gophercloud.RequestOpts{
		OkCodes: []int{http.StatusOK},
	})
	if err != nil {
		tflog.Error(ctx, "Image usage read error.", map[string]any{"error": err.Error()})
		return 0, fmt.Errorf("failed to get image usage: %w", err)
	}
	return body.Images.Size, nil
}

// ValidImageQuota はドキュメントの刻み（50GB に 500GB 単位で加える）に合うかを返す.
func ValidImageQuota(gb int64) bool {
	return gb >= ImageQuotaDefaultGB && (gb-ImageQuotaDefaultGB)%ImageQuotaStepGB == 0
}

// FormatImageSize は GB を API の形（"550GB"）にする.
func FormatImageSize(gb int64) string {
	return strconv.FormatInt(gb, 10) + "GB"
}

// ParseImageSize は API の形（"550GB"）を GB にする.
func ParseImageSize(s string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "GB")), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse the image size %q: %w", s, err)
	}
	return n, nil
}
