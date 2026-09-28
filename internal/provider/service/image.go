// イメージの API を提供する.

package service

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/images"
	"github.com/gophercloud/gophercloud/v2/openstack/utils"
	"github.com/gophercloud/gophercloud/v2/pagination"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// イメージ一覧の1件のうち、データソースが使う項目.
// 日時（created_at など）は API 仕様の応答例がタイムゾーンを含まない形式で、virtual_size は文字列型とされているため、
// gophercloud の images.Image（とそれを使う images.List のページ）では読み込みに失敗しうる. 使う項目だけを読む.
type Image struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	MinDisk      int    `json:"min_disk"`
	MinRAM       int    `json:"min_ram"`
	Size         int64  `json:"size"`
	Visibility   string `json:"visibility"`
	OSType       string `json:"os_type"`
	OSVersion    string `json:"os_version"`
	Architecture string `json:"architecture"`
}

// ListImagesByName 名前が一致するイメージの一覧を取得.
// 一覧は limit・marker でページに分かれるため、next のリンクをたどってすべてのページを読む.
func (c *ConohaClient) ListImagesByName(ctx context.Context, name string) ([]Image, error) {
	if c.ImageClient == nil {
		return nil, fmt.Errorf("image client is not initialized")
	}

	if c.ImageClient.ProviderClient == nil && c.ProviderClient != nil {
		c.ImageClient.ProviderClient = c.ProviderClient
	}

	tflog.Debug(ctx, "Listing images.", map[string]any{"name": name})

	query, err := images.ListOpts{Name: name}.ToImageListQuery()
	if err != nil {
		return nil, fmt.Errorf("failed to build image list query: %w", err)
	}

	var list []Image
	pager := pagination.NewPager(c.ImageClient, c.ImageClient.ServiceURL("images")+query, func(r pagination.PageResult) pagination.Page {
		return imagePage{pagination.LinkedPageBase{PageResult: r}}
	})
	err = pager.EachPage(ctx, func(_ context.Context, page pagination.Page) (bool, error) {
		found, err := extractImages(page)
		if err != nil {
			return false, err
		}
		list = append(list, found...)
		return true, nil
	})
	if err != nil {
		tflog.Error(ctx, "Image listing error.", map[string]any{"error": err.Error(), "name": name})
		return nil, fmt.Errorf("failed to list images: %w", err)
	}

	return list, nil
}

// イメージ一覧の1ページ.
type imagePage struct {
	pagination.LinkedPageBase
}

// ページにイメージが無ければ空とする.
func (p imagePage) IsEmpty() (bool, error) {
	if p.StatusCode == 204 {
		return true, nil
	}
	found, err := extractImages(p)
	return len(found) == 0, err
}

// 応答の next（/v2/images?marker=... のような、エンドポイントのバージョンからのパス）から次のページの URL を組み立てる.
func (p imagePage) NextPageURL(endpointURL string) (string, error) {
	var s struct {
		Next string `json:"next"`
	}
	if err := p.ExtractInto(&s); err != nil {
		return "", err
	}
	if s.Next == "" {
		return "", nil
	}

	base, err := utils.BaseEndpoint(endpointURL)
	if err != nil {
		return "", err
	}
	next, err := url.Parse(s.Next)
	if err != nil {
		return "", err
	}
	nextURL, err := url.Parse(gophercloud.NormalizeURL(base) + strings.TrimPrefix(next.Path, "/"))
	if err != nil {
		return "", err
	}
	nextURL.RawQuery = next.RawQuery
	return nextURL.String(), nil
}

// ページからイメージを取り出す.
func extractImages(page pagination.Page) ([]Image, error) {
	var s struct {
		Images []Image `json:"images"`
	}
	err := page.(imagePage).ExtractInto(&s)
	return s.Images, err
}
