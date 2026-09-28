// ローカルネットワーク・サブネット・ポート・追加IP・ポートのアタッチと QoS ポリシーの API の呼び出しを提供する.
// リクエストの本文は ConoHa の OpenAPI 定義（gmo-internet/conoha_vps_openapi）に載っている項目だけを送るため、
// gophercloud の Opts を通さずに組み立てる.

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/attachinterfaces"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Network はネットワークのレスポンス.
type Network struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Status  string   `json:"status"`
	MTU     int      `json:"mtu"`
	Subnets []string `json:"subnets"`
}

// Subnet はサブネットのレスポンス.
type Subnet struct {
	ID              string                 `json:"id"`
	Name            string                 `json:"name"`
	NetworkID       string                 `json:"network_id"`
	CIDR            string                 `json:"cidr"`
	IPVersion       int                    `json:"ip_version"`
	GatewayIP       *string                `json:"gateway_ip"`
	AllocationPools []SubnetAllocationPool `json:"allocation_pools"`
}

// SubnetAllocationPool はサブネットの割り当て範囲.
type SubnetAllocationPool struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// SubnetCreateOpts はサブネット作成（ローカルネットワーク用）のリクエスト.
type SubnetCreateOpts struct {
	NetworkID string `json:"network_id"`
	CIDR      string `json:"cidr"`
}

// Port はポートのレスポンス（PortRes・ListPortsRes の port）.
type Port struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	NetworkID           string            `json:"network_id"`
	MACAddress          string            `json:"mac_address"`
	Status              string            `json:"status"`
	DeviceID            string            `json:"device_id"`
	FixedIPs            []PortFixedIP     `json:"fixed_ips"`
	SecurityGroups      []string          `json:"security_groups"`
	AllowedAddressPairs []PortAddressPair `json:"allowed_address_pairs"`
	// QoSPolicyID はポートに付けた QoS ポリシー. 付けていなければ null.
	QoSPolicyID *string `json:"qos_policy_id"`
	// QoSNetworkPolicyID はネットワークに付いた QoS ポリシー（ポートのものが無いときに効く）. 付いていなければ null.
	QoSNetworkPolicyID *string `json:"qos_network_policy_id"`
	// HasQoSPolicyID はレスポンスに qos_policy_id の項目があったか（null を含む）.
	HasQoSPolicyID bool `json:"-"`
}

// UnmarshalJSON は、qos_policy_id が null なのか項目ごと無いのかを区別して読む.
func (p *Port) UnmarshalJSON(b []byte) error {
	type plain Port
	var v struct {
		plain
		QoSPolicyID json.RawMessage `json:"qos_policy_id"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*p = Port(v.plain)
	p.QoSPolicyID = nil
	p.HasQoSPolicyID = v.QoSPolicyID != nil
	if v.QoSPolicyID != nil && string(v.QoSPolicyID) != "null" {
		var id string
		if err := json.Unmarshal(v.QoSPolicyID, &id); err != nil {
			return fmt.Errorf("qos_policy_id: %w", err)
		}
		p.QoSPolicyID = &id
	}
	return nil
}

// PortFixedIP はポートに割り当てる IP アドレス. ip_address を省くとサブネットから自動で割り当てられる.
type PortFixedIP struct {
	SubnetID  string `json:"subnet_id,omitempty"`
	IPAddress string `json:"ip_address,omitempty"`
}

// PortAddressPair は VIP として使う IP アドレスまたはネットワークアドレス（CIDR 形式）.
// リクエストは ip_address だけを送る（OpenAPI 定義のリクエストに mac_address は無い）.
type PortAddressPair struct {
	IPAddress string `json:"ip_address"`
}

// UnmarshalJSON は、オブジェクト（{"ip_address": ...}）と文字列のどちらの形でも読む.
// OpenAPI 定義はレスポンスの要素を文字列と書くが、Neutron の実装はオブジェクトを返すため両方を受ける.
func (a *PortAddressPair) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		a.IPAddress = s
		return nil
	}
	var v struct {
		IPAddress string `json:"ip_address"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	a.IPAddress = v.IPAddress
	return nil
}

// PortCreateOpts はポート作成（ローカルネットワーク用）のリクエスト.
type PortCreateOpts struct {
	NetworkID           string            `json:"network_id"`
	FixedIPs            []PortFixedIP     `json:"fixed_ips,omitempty"`
	SecurityGroups      []string          `json:"security_groups,omitempty"`
	AllowedAddressPairs []PortAddressPair `json:"allowed_address_pairs,omitempty"`
}

// PortUpdateOpts はポート更新のリクエスト. nil の項目は送らない（指定した項目だけが更新される）.
type PortUpdateOpts struct {
	FixedIPs            *[]PortFixedIP     `json:"fixed_ips,omitempty"`
	SecurityGroups      *[]string          `json:"security_groups,omitempty"`
	QoSPolicyID         *string            `json:"qos_policy_id,omitempty"`
	AllowedAddressPairs *[]PortAddressPair `json:"allowed_address_pairs,omitempty"`
}

// AllocateIPsOpts はポート作成（追加IP用）のリクエスト. OpenAPI 定義のリクエストは qos_policy_id を取らない.
type AllocateIPsOpts struct {
	Count          int      `json:"count"`
	SecurityGroups []string `json:"security_groups,omitempty"`
}

// QoSPolicy は QoS ポリシーのレスポンス.
type QoSPolicy struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Shared      bool            `json:"shared"`
	IsDefault   bool            `json:"is_default"`
	Rules       []QoSPolicyRule `json:"rules"`
	Tags        []string        `json:"tags"`
}

// QoSPolicyRule は QoS ポリシーの帯域制限ルール.
type QoSPolicyRule struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	Direction    string `json:"direction"`
	MaxKbps      int64  `json:"max_kbps"`
	MaxBurstKbps int64  `json:"max_burst_kbps"`
}

func (c *ConohaClient) localNetworkClient() (*gophercloud.ServiceClient, error) {
	if c.NetworkClient == nil {
		return nil, fmt.Errorf("network client is not initialized")
	}
	if c.NetworkClient.ProviderClient == nil && c.ProviderClient != nil {
		c.NetworkClient.ProviderClient = c.ProviderClient
	}
	return c.NetworkClient, nil
}

func (c *ConohaClient) portAttachClient() (*gophercloud.ServiceClient, error) {
	if c.ComputeClient == nil {
		return nil, fmt.Errorf("compute client is not initialized")
	}
	if c.ComputeClient.ProviderClient == nil && c.ProviderClient != nil {
		c.ComputeClient.ProviderClient = c.ProviderClient
	}
	return c.ComputeClient, nil
}

// ネットワーク API に JSON を送り、レスポンスを out に読む.
func (c *ConohaClient) localNetworkRequest(ctx context.Context, method, what string, body, out any, okCodes []int, path ...string) error {
	return c.localNetworkRequestQuery(ctx, method, what, nil, body, out, okCodes, path...)
}

// localNetworkRequest の、クエリ文字列を付けるもの.
func (c *ConohaClient) localNetworkRequestQuery(ctx context.Context, method, what string, query url.Values, body, out any, okCodes []int, path ...string) error {
	client, err := c.localNetworkClient()
	if err != nil {
		return err
	}

	tflog.Debug(ctx, "Sending network API request.", map[string]any{"method": method, "target": what})

	target := client.ServiceURL(path...)
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	opts := &gophercloud.RequestOpts{OkCodes: okCodes}
	switch method {
	case "POST":
		_, err = client.Post(ctx, target, body, out, opts)
	case "PUT":
		_, err = client.Put(ctx, target, body, out, opts)
	case "GET":
		_, err = client.Get(ctx, target, out, opts)
	case "DELETE":
		_, err = client.Delete(ctx, target, opts)
	default:
		err = fmt.Errorf("unsupported method %s", method)
	}
	if err != nil {
		tflog.Error(ctx, "Network API request error.", map[string]any{"method": method, "target": what, "error": err.Error()})
		return fmt.Errorf("failed to %s: %w", what, err)
	}
	return nil
}

// CreateLocalNetwork ローカルネットワークを作成（リクエストの本文は無い）.
func (c *ConohaClient) CreateLocalNetwork(ctx context.Context) (*Network, error) {
	var out struct {
		Network Network `json:"network"`
	}
	if err := c.localNetworkRequest(ctx, "POST", "create network", nil, &out, []int{201}, "networks"); err != nil {
		return nil, err
	}
	return &out.Network, nil
}

// GetNetwork ネットワークの詳細を取得.
func (c *ConohaClient) GetNetwork(ctx context.Context, id string) (*Network, error) {
	var out struct {
		Network Network `json:"network"`
	}
	if err := c.localNetworkRequest(ctx, "GET", "retrieve network", nil, &out, []int{200}, "networks", id); err != nil {
		return nil, err
	}
	return &out.Network, nil
}

// DeleteNetwork ネットワークを削除. サブネットやポートが残っていると削除できない.
func (c *ConohaClient) DeleteNetwork(ctx context.Context, id string) error {
	return c.localNetworkRequest(ctx, "DELETE", "delete network", nil, nil, []int{204}, "networks", id)
}

// CreateSubnet ローカルネットワーク用のサブネットを作成.
func (c *ConohaClient) CreateSubnet(ctx context.Context, opts SubnetCreateOpts) (*Subnet, error) {
	var out struct {
		Subnet Subnet `json:"subnet"`
	}
	body := map[string]any{"subnet": opts}
	if err := c.localNetworkRequest(ctx, "POST", "create subnet", body, &out, []int{201}, "subnets"); err != nil {
		return nil, err
	}
	return &out.Subnet, nil
}

// GetSubnet サブネットの詳細を取得.
func (c *ConohaClient) GetSubnet(ctx context.Context, id string) (*Subnet, error) {
	var out struct {
		Subnet Subnet `json:"subnet"`
	}
	if err := c.localNetworkRequest(ctx, "GET", "retrieve subnet", nil, &out, []int{200}, "subnets", id); err != nil {
		return nil, err
	}
	return &out.Subnet, nil
}

// DeleteSubnet サブネットを削除.
func (c *ConohaClient) DeleteSubnet(ctx context.Context, id string) error {
	return c.localNetworkRequest(ctx, "DELETE", "delete subnet", nil, nil, []int{204}, "subnets", id)
}

// CreatePort ローカルネットワーク用のポートを作成.
func (c *ConohaClient) CreatePort(ctx context.Context, opts PortCreateOpts) (*Port, error) {
	var out struct {
		Port Port `json:"port"`
	}
	body := map[string]any{"port": opts}
	if err := c.localNetworkRequest(ctx, "POST", "create port", body, &out, []int{201}, "ports"); err != nil {
		return nil, err
	}
	return &out.Port, nil
}

// AllocateIPs 追加IPアドレス用のポートを作成.
func (c *ConohaClient) AllocateIPs(ctx context.Context, opts AllocateIPsOpts) (*Port, error) {
	var out struct {
		Port Port `json:"port"`
	}
	body := map[string]any{"allocateip": opts}
	if err := c.localNetworkRequest(ctx, "POST", "allocate additional IPs", body, &out, []int{201}, "allocateips"); err != nil {
		return nil, err
	}
	return &out.Port, nil
}

// GetPort ポートの詳細を取得.
func (c *ConohaClient) GetPort(ctx context.Context, id string) (*Port, error) {
	var out struct {
		Port Port `json:"port"`
	}
	if err := c.localNetworkRequest(ctx, "GET", "retrieve port", nil, &out, []int{200}, "ports", id); err != nil {
		return nil, err
	}
	return &out.Port, nil
}

// UpdatePort ポートを更新. 指定した項目だけが更新される.
func (c *ConohaClient) UpdatePort(ctx context.Context, id string, opts PortUpdateOpts) (*Port, error) {
	var out struct {
		Port Port `json:"port"`
	}
	body := map[string]any{"port": opts}
	if err := c.localNetworkRequest(ctx, "PUT", "update port", body, &out, []int{200}, "ports", id); err != nil {
		return nil, err
	}
	return &out.Port, nil
}

// DeletePort ポートを削除. サーバーにアタッチされているポートは削除できない.
func (c *ConohaClient) DeletePort(ctx context.Context, id string) error {
	return c.localNetworkRequest(ctx, "DELETE", "delete port", nil, nil, []int{204}, "ports", id)
}

// ListQoSPolicies QoS ポリシーの一覧を取得. name が空でなければ、その名前で絞る（クエリの name）.
func (c *ConohaClient) ListQoSPolicies(ctx context.Context, name string) ([]QoSPolicy, error) {
	var out struct {
		Policies []QoSPolicy `json:"policies"`
	}
	var query url.Values
	if name != "" {
		query = url.Values{"name": {name}}
	}
	if err := c.localNetworkRequestQuery(ctx, "GET", "list QoS policies", query, nil, &out, []int{200}, "qos", "policies"); err != nil {
		return nil, err
	}
	return out.Policies, nil
}

// GetQoSPolicy QoS ポリシーの詳細を取得.
func (c *ConohaClient) GetQoSPolicy(ctx context.Context, id string) (*QoSPolicy, error) {
	var out struct {
		Policy QoSPolicy `json:"policy"`
	}
	if err := c.localNetworkRequest(ctx, "GET", "retrieve QoS policy", nil, &out, []int{200}, "qos", "policies", id); err != nil {
		return nil, err
	}
	return &out.Policy, nil
}

// AttachPort ポートをサーバーにアタッチ. アタッチは同期（200）で、レスポンスの時点で付いている.
func (c *ConohaClient) AttachPort(ctx context.Context, serverID, portID string) (*attachinterfaces.Interface, error) {
	client, err := c.portAttachClient()
	if err != nil {
		return nil, err
	}

	tflog.Debug(ctx, "Attaching port.", map[string]any{"server_id": serverID, "port_id": portID})

	iface, err := attachinterfaces.Create(ctx, client, serverID, attachinterfaces.CreateOpts{PortID: portID}).Extract()
	if err != nil {
		tflog.Error(ctx, "Port attach error.", map[string]any{"server_id": serverID, "port_id": portID, "error": err.Error()})
		return nil, fmt.Errorf("failed to attach port: %w", err)
	}
	return iface, nil
}

// GetAttachedPort サーバーにアタッチされているポートの詳細を取得.
func (c *ConohaClient) GetAttachedPort(ctx context.Context, serverID, portID string) (*attachinterfaces.Interface, error) {
	client, err := c.portAttachClient()
	if err != nil {
		return nil, err
	}

	iface, err := attachinterfaces.Get(ctx, client, serverID, portID).Extract()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve attached port: %w", err)
	}
	return iface, nil
}

// DetachPort サーバーからポートをデタッチし、アタッチ済みポートから消えるまで待つ.
// デタッチは非同期（202）で、消える前にポートを削除すると失敗するため待機する.
func (c *ConohaClient) DetachPort(ctx context.Context, serverID, portID string, timeout time.Duration) error {
	client, err := c.portAttachClient()
	if err != nil {
		return err
	}

	tflog.Debug(ctx, "Detaching port.", map[string]any{"server_id": serverID, "port_id": portID})

	_, err = client.Delete(ctx, client.ServiceURL("servers", serverID, "os-interface", portID), &gophercloud.RequestOpts{OkCodes: []int{202}})
	if err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			return nil
		}
		tflog.Error(ctx, "Port detach error.", map[string]any{"server_id": serverID, "port_id": portID, "error": err.Error()})
		return fmt.Errorf("failed to detach port: %w", err)
	}

	deadline := time.Now().Add(timeout)
	for {
		_, err := c.GetAttachedPort(ctx, serverID, portID)
		if gophercloud.ResponseCodeIs(err, 404) {
			return nil
		}
		if err != nil {
			tflog.Warn(ctx, "Attached port check error.", map[string]any{"error": err.Error()})
		}
		if time.Now().After(deadline) {
			return errors.New("timeout: waiting for the port to be detached from the server")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(DetachPollInterval):
		}
	}
}

// DetachPollInterval は非同期の処理の完了を確かめる間隔.
var DetachPollInterval = 2 * time.Second
