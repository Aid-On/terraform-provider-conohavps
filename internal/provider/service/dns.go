// DNS（ドメインとレコード）の API を提供する.
// ConoHa の DNS API は OpenStack Designate v2 ではなく独自の v1 のため、
// gophercloud の dns パッケージを使わずにリクエストを直接組み立てる.

package service

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// DNS の一覧取得で1回に取る件数.
const dnsListPageSize = 100

// DNSDomain は DNS に登録したドメイン.
type DNSDomain struct {
	ID        string `json:"uuid"`
	Name      string `json:"name"`
	ProjectID string `json:"project_id"`
	Serial    int64  `json:"serial"`
	TTL       int64  `json:"ttl"`
	Email     string `json:"email"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// DNSDomainCreateOpts はドメイン情報登録のリクエスト本文.
type DNSDomainCreateOpts struct {
	Name  string `json:"name"`
	TTL   int64  `json:"ttl"`
	Email string `json:"email"`
}

// DNSDomainUpdateOpts はドメイン情報更新のリクエスト本文.
type DNSDomainUpdateOpts struct {
	TTL   int64  `json:"ttl"`
	Email string `json:"email"`
}

// DNSRecord はドメインに設定した DNS レコード.
type DNSRecord struct {
	ID        string `json:"uuid"`
	DomainID  string `json:"domain_uuid"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Data      string `json:"data"`
	Priority  *int64 `json:"priority"`
	Weight    *int64 `json:"weight"`
	Port      *int64 `json:"port"`
	TTL       *int64 `json:"ttl"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// DNSRecordOpts はレコード作成・更新のリクエスト本文.
// priority・weight・port は MX や SRV のときだけ送る.
type DNSRecordOpts struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Data     string `json:"data"`
	Priority *int64 `json:"priority,omitempty"`
	Weight   *int64 `json:"weight,omitempty"`
	Port     *int64 `json:"port,omitempty"`
}

// DNS のクライアントを返す. カタログに DNS が無ければエラーを返す.
func (c *ConohaClient) dnsClient() (*gophercloud.ServiceClient, error) {
	if c == nil || c.DNSClient == nil {
		return nil, ServiceUnavailable("dns")
	}
	return c.DNSClient, nil
}

// CreateDNSDomain はドメインを登録する.
func (c *ConohaClient) CreateDNSDomain(ctx context.Context, opts DNSDomainCreateOpts) (*DNSDomain, error) {
	client, err := c.dnsClient()
	if err != nil {
		return nil, err
	}

	tflog.Debug(ctx, "Creating DNS domain.", map[string]any{"name": opts.Name})

	var out DNSDomain
	if _, err := client.Post(ctx, client.ServiceURL("domains"), opts, &out, &gophercloud.RequestOpts{
		OkCodes: []int{200, 201},
	}); err != nil {
		tflog.Error(ctx, "DNS domain creation error.", map[string]any{"error": err.Error()})
		return nil, fmt.Errorf("failed to create DNS domain: %w", err)
	}
	return &out, nil
}

// GetDNSDomain はドメインの詳細を取得する.
func (c *ConohaClient) GetDNSDomain(ctx context.Context, domainID string) (*DNSDomain, error) {
	client, err := c.dnsClient()
	if err != nil {
		return nil, err
	}

	tflog.Debug(ctx, "Retrieving DNS domain.", map[string]any{"id": domainID})

	var out DNSDomain
	if _, err := client.Get(ctx, client.ServiceURL("domains", domainID), &out, &gophercloud.RequestOpts{
		OkCodes: []int{200},
	}); err != nil {
		return nil, fmt.Errorf("failed to retrieve DNS domain: %w", err)
	}
	return &out, nil
}

// ListDNSDomains はドメインの一覧をすべて取得する.
func (c *ConohaClient) ListDNSDomains(ctx context.Context) ([]DNSDomain, error) {
	client, err := c.dnsClient()
	if err != nil {
		return nil, err
	}

	tflog.Debug(ctx, "Listing DNS domains.", map[string]any{})

	var all []DNSDomain
	for {
		var page struct {
			Domains    []DNSDomain `json:"domains"`
			TotalCount int         `json:"total_count"`
		}
		if _, err := client.Get(ctx, dnsPagedURL(client.ServiceURL("domains"), len(all)), &page, &gophercloud.RequestOpts{
			OkCodes: []int{200},
		}); err != nil {
			tflog.Error(ctx, "DNS domain listing error.", map[string]any{"error": err.Error()})
			return nil, fmt.Errorf("failed to list DNS domains: %w", err)
		}
		all = append(all, page.Domains...)
		if len(page.Domains) == 0 || len(all) >= page.TotalCount {
			return all, nil
		}
	}
}

// UpdateDNSDomain はドメインの TTL と連絡先メールアドレスを更新する.
func (c *ConohaClient) UpdateDNSDomain(ctx context.Context, domainID string, opts DNSDomainUpdateOpts) (*DNSDomain, error) {
	client, err := c.dnsClient()
	if err != nil {
		return nil, err
	}

	tflog.Debug(ctx, "Updating DNS domain.", map[string]any{"id": domainID})

	var out DNSDomain
	if _, err := client.Put(ctx, client.ServiceURL("domains", domainID), opts, &out, &gophercloud.RequestOpts{
		OkCodes: []int{200},
	}); err != nil {
		tflog.Error(ctx, "DNS domain update error.", map[string]any{"error": err.Error()})
		return nil, fmt.Errorf("failed to update DNS domain: %w", err)
	}
	return &out, nil
}

// DeleteDNSDomain はドメインを削除する.
func (c *ConohaClient) DeleteDNSDomain(ctx context.Context, domainID string) error {
	client, err := c.dnsClient()
	if err != nil {
		return err
	}

	tflog.Debug(ctx, "Deleting DNS domain.", map[string]any{"id": domainID})

	if _, err := client.Delete(ctx, client.ServiceURL("domains", domainID), &gophercloud.RequestOpts{
		OkCodes: []int{200, 202, 204},
	}); err != nil {
		return fmt.Errorf("failed to delete DNS domain: %w", err)
	}
	return nil
}

// CreateDNSRecord はドメインにレコードを作成する.
func (c *ConohaClient) CreateDNSRecord(ctx context.Context, domainID string, opts DNSRecordOpts) (*DNSRecord, error) {
	client, err := c.dnsClient()
	if err != nil {
		return nil, err
	}

	tflog.Debug(ctx, "Creating DNS record.", map[string]any{"domain_id": domainID, "name": opts.Name, "type": opts.Type})

	var out DNSRecord
	if _, err := client.Post(ctx, client.ServiceURL("domains", domainID, "records"), opts, &out, &gophercloud.RequestOpts{
		OkCodes: []int{200, 201},
	}); err != nil {
		tflog.Error(ctx, "DNS record creation error.", map[string]any{"error": err.Error()})
		return nil, fmt.Errorf("failed to create DNS record: %w", err)
	}
	return &out, nil
}

// GetDNSRecord はレコードの詳細を取得する.
func (c *ConohaClient) GetDNSRecord(ctx context.Context, domainID, recordID string) (*DNSRecord, error) {
	client, err := c.dnsClient()
	if err != nil {
		return nil, err
	}

	tflog.Debug(ctx, "Retrieving DNS record.", map[string]any{"domain_id": domainID, "id": recordID})

	var out DNSRecord
	if _, err := client.Get(ctx, client.ServiceURL("domains", domainID, "records", recordID), &out, &gophercloud.RequestOpts{
		OkCodes: []int{200},
	}); err != nil {
		return nil, fmt.Errorf("failed to retrieve DNS record: %w", err)
	}
	return &out, nil
}

// ListDNSRecords はドメインのレコードの一覧をすべて取得する.
func (c *ConohaClient) ListDNSRecords(ctx context.Context, domainID string) ([]DNSRecord, error) {
	client, err := c.dnsClient()
	if err != nil {
		return nil, err
	}

	tflog.Debug(ctx, "Listing DNS records.", map[string]any{"domain_id": domainID})

	var all []DNSRecord
	for {
		var page struct {
			Records    []DNSRecord `json:"records"`
			TotalCount int         `json:"total_count"`
		}
		if _, err := client.Get(ctx, dnsPagedURL(client.ServiceURL("domains", domainID, "records"), len(all)), &page, &gophercloud.RequestOpts{
			OkCodes: []int{200},
		}); err != nil {
			tflog.Error(ctx, "DNS record listing error.", map[string]any{"error": err.Error()})
			return nil, fmt.Errorf("failed to list DNS records: %w", err)
		}
		all = append(all, page.Records...)
		if len(page.Records) == 0 || len(all) >= page.TotalCount {
			return all, nil
		}
	}
}

// UpdateDNSRecord はレコードを更新する.
func (c *ConohaClient) UpdateDNSRecord(ctx context.Context, domainID, recordID string, opts DNSRecordOpts) (*DNSRecord, error) {
	client, err := c.dnsClient()
	if err != nil {
		return nil, err
	}

	tflog.Debug(ctx, "Updating DNS record.", map[string]any{"domain_id": domainID, "id": recordID})

	var out DNSRecord
	if _, err := client.Put(ctx, client.ServiceURL("domains", domainID, "records", recordID), opts, &out, &gophercloud.RequestOpts{
		OkCodes: []int{200},
	}); err != nil {
		tflog.Error(ctx, "DNS record update error.", map[string]any{"error": err.Error()})
		return nil, fmt.Errorf("failed to update DNS record: %w", err)
	}
	return &out, nil
}

// DeleteDNSRecord はレコードを削除する.
func (c *ConohaClient) DeleteDNSRecord(ctx context.Context, domainID, recordID string) error {
	client, err := c.dnsClient()
	if err != nil {
		return err
	}

	tflog.Debug(ctx, "Deleting DNS record.", map[string]any{"domain_id": domainID, "id": recordID})

	if _, err := client.Delete(ctx, client.ServiceURL("domains", domainID, "records", recordID), &gophercloud.RequestOpts{
		OkCodes: []int{200, 202, 204},
	}); err != nil {
		return fmt.Errorf("failed to delete DNS record: %w", err)
	}
	return nil
}

// 一覧取得の URL に件数と開始位置を付ける（offset は limit と併せて使う）.
func dnsPagedURL(base string, offset int) string {
	q := url.Values{}
	q.Set("limit", strconv.Itoa(dnsListPageSize))
	q.Set("offset", strconv.Itoa(offset))
	return base + "?" + q.Encode()
}
