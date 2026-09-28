// ロードバランサー（LBaaS）の API の呼び出しを提供する.
// リクエストの本文は GMO の OpenAPI 仕様（conoha_vps_openapi）の要求スキーマに載っている項目だけを送るため、
// gophercloud の loadbalancer パッケージは使わず、カタログのエンドポイントへ直接リクエストする.
//
// ロードバランサー配下（リスナー・プール・メンバー・ヘルスモニタ）の変更は、ロードバランサーの
// provisioning_status が PENDING_* の間は 409 で拒否される. そのため変更の前後でロードバランサーが
// ACTIVE になるのを待ち、変更の直後に拒否されたときはロードバランサーが PENDING_* なら待ってやり直す.

package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

const (
	provisioningActive  = "ACTIVE"
	provisioningError   = "ERROR"
	provisioningDeleted = "DELETED"
)

// LBRef は応答に含まれる、紐づくリソースの ID.
type LBRef struct {
	ID string `json:"id"`
}

// LBStatus は LBaaS の各リソースに共通する状態.
type LBStatus struct {
	ProvisioningStatus string `json:"provisioning_status"`
	OperatingStatus    string `json:"operating_status"`
}

// LoadBalancer はロードバランサー.
type LoadBalancer struct {
	LBStatus
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	AdminStateUp bool   `json:"admin_state_up"`
	VipAddress   string `json:"vip_address"`
	VipPortID    string `json:"vip_port_id"`
	VipSubnetID  string `json:"vip_subnet_id"`
	VipNetworkID string `json:"vip_network_id"`
}

// LBListener はリスナー. connection_limit の -1 は無制限を表す.
type LBListener struct {
	LBStatus
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Protocol        string  `json:"protocol"`
	ProtocolPort    int     `json:"protocol_port"`
	ConnectionLimit int     `json:"connection_limit"`
	DefaultPoolID   *string `json:"default_pool_id"`
	AdminStateUp    bool    `json:"admin_state_up"`
	LoadBalancers   []LBRef `json:"loadbalancers"`
}

// LoadBalancerID はリスナーが紐づくロードバランサーの ID を返す.
func (l *LBListener) LoadBalancerID() string {
	return firstRef(l.LoadBalancers)
}

// LBPool はプール.
// 応答の members は、仕様では ID の文字列の配列、OpenStack では {"id": ...} の配列と形が定まらないため読まない.
type LBPool struct {
	LBStatus
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Protocol      string  `json:"protocol"`
	LBAlgorithm   string  `json:"lb_algorithm"`
	AdminStateUp  bool    `json:"admin_state_up"`
	LoadBalancers []LBRef `json:"loadbalancers"`
	Listeners     []LBRef `json:"listeners"`
}

// LoadBalancerID はプールが紐づくロードバランサーの ID を返す.
func (p *LBPool) LoadBalancerID() string {
	return firstRef(p.LoadBalancers)
}

// ListenerID はプールが紐づくリスナーの ID を返す.
func (p *LBPool) ListenerID() string {
	return firstRef(p.Listeners)
}

// LBMember はプールのメンバー.
type LBMember struct {
	LBStatus
	ID           string `json:"id"`
	Name         string `json:"name"`
	Address      string `json:"address"`
	ProtocolPort int    `json:"protocol_port"`
	Weight       int    `json:"weight"`
	AdminStateUp bool   `json:"admin_state_up"`
}

// LBHealthMonitor はヘルスモニタ.
// TCP・PING のヘルスモニタでは url_path と expected_codes が null で返る.
type LBHealthMonitor struct {
	LBStatus
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Type          string  `json:"type"`
	Delay         int     `json:"delay"`
	Timeout       int     `json:"timeout"`
	MaxRetries    int     `json:"max_retries"`
	URLPath       *string `json:"url_path"`
	ExpectedCodes *string `json:"expected_codes"`
	AdminStateUp  bool    `json:"admin_state_up"`
	Pools         []LBRef `json:"pools"`
}

// PoolID はヘルスモニタが紐づくプールの ID を返す.
func (m *LBHealthMonitor) PoolID() string {
	return firstRef(m.Pools)
}

// CreateLoadBalancerOpts はロードバランサー追加のリクエスト.
type CreateLoadBalancerOpts struct {
	Name string `json:"name"`
}

// UpdateLoadBalancerOpts はロードバランサー更新のリクエスト.
type UpdateLoadBalancerOpts struct {
	Name string `json:"name"`
}

// CreateListenerOpts はリスナー作成のリクエスト.
type CreateListenerOpts struct {
	Protocol       string `json:"protocol"`
	ProtocolPort   int    `json:"protocol_port"`
	LoadBalancerID string `json:"loadbalancer_id"`
	Name           string `json:"name"`
}

// UpdateListenerOpts はリスナー更新のリクエスト.
type UpdateListenerOpts struct {
	Name string `json:"name"`
}

// CreatePoolOpts はプール作成のリクエスト.
type CreatePoolOpts struct {
	LBAlgorithm string `json:"lb_algorithm"`
	Protocol    string `json:"protocol"`
	ListenerID  string `json:"listener_id"`
	Name        string `json:"name"`
}

// UpdatePoolOpts はプール更新のリクエスト. 変える項目だけを送る.
type UpdatePoolOpts struct {
	LBAlgorithm *string `json:"lb_algorithm,omitempty"`
	Name        *string `json:"name,omitempty"`
}

// CreateMemberOpts はメンバー追加のリクエスト.
type CreateMemberOpts struct {
	Name         string `json:"name"`
	Address      string `json:"address"`
	ProtocolPort int    `json:"protocol_port"`
}

// UpdateMemberOpts はメンバー更新のリクエスト.
// 仕様の要求スキーマは admin_state_up を string と書くが、例・説明・応答はいずれも真偽値なので真偽値で送る.
type UpdateMemberOpts struct {
	AdminStateUp bool `json:"admin_state_up"`
}

// CreateHealthMonitorOpts はヘルスモニタ作成のリクエスト.
// url_path と expected_codes は省略でき、HTTP・HTTPS で指定されたときだけ送る.
type CreateHealthMonitorOpts struct {
	Name          string `json:"name"`
	PoolID        string `json:"pool_id"`
	Delay         int    `json:"delay"`
	MaxRetries    int    `json:"max_retries"`
	Timeout       int    `json:"timeout"`
	Type          string `json:"type"`
	URLPath       string `json:"url_path,omitempty"`
	ExpectedCodes string `json:"expected_codes,omitempty"`
}

// UpdateHealthMonitorOpts はヘルスモニタ更新のリクエスト.
type UpdateHealthMonitorOpts struct {
	Name string `json:"name"`
}

// ロードバランサー.

// CreateLoadBalancer はロードバランサーを追加し、ACTIVE になるまで待つ.
// 追加できたが待機に失敗したときは、追加したロードバランサーとエラーの両方を返す.
func (c *ConohaClient) CreateLoadBalancer(ctx context.Context, opts CreateLoadBalancerOpts) (*LoadBalancer, error) {
	created, err := lbCreate[LoadBalancer](ctx, c, "loadbalancer", opts, "loadbalancers")
	if err != nil {
		return nil, err
	}
	if err := c.WaitForLoadBalancerActive(ctx, created.ID); err != nil {
		return created, err
	}
	lb, err := c.GetLoadBalancer(ctx, created.ID)
	if err != nil {
		return created, err
	}
	return lb, nil
}

// GetLoadBalancer はロードバランサーの詳細を取得する.
func (c *ConohaClient) GetLoadBalancer(ctx context.Context, id string) (*LoadBalancer, error) {
	return lbGet[LoadBalancer](ctx, c, "loadbalancer", "loadbalancers", id)
}

// UpdateLoadBalancer はロードバランサー名を更新し、ACTIVE になるまで待つ.
func (c *ConohaClient) UpdateLoadBalancer(ctx context.Context, id string, opts UpdateLoadBalancerOpts) (*LoadBalancer, error) {
	err := c.changeUnderLoadBalancer(ctx, id, func() error {
		_, err := lbUpdate[LoadBalancer](ctx, c, "loadbalancer", opts, "loadbalancers", id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return c.GetLoadBalancer(ctx, id)
}

// DeleteLoadBalancer はロードバランサーを削除し、消えるまで待つ.
func (c *ConohaClient) DeleteLoadBalancer(ctx context.Context, id string) error {
	err := c.changeUnderLoadBalancer(ctx, id, func() error {
		return lbDelete(ctx, c, "loadbalancers", id)
	})
	if err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			return nil
		}
		return err
	}
	return waitGone(ctx, "load balancer", id, func(ctx context.Context) (string, error) {
		lb, err := c.GetLoadBalancer(ctx, id)
		if err != nil {
			return "", err
		}
		return lb.ProvisioningStatus, nil
	})
}

// WaitForLoadBalancerActive はロードバランサーの provisioning_status が ACTIVE になるまで待つ.
func (c *ConohaClient) WaitForLoadBalancerActive(ctx context.Context, id string) error {
	return waitActive(ctx, "load balancer", id, func(ctx context.Context) (string, error) {
		lb, err := c.GetLoadBalancer(ctx, id)
		if err != nil {
			return "", err
		}
		return lb.ProvisioningStatus, nil
	})
}

// リスナー.

// CreateListener はリスナーを作成し、リスナーとロードバランサーが ACTIVE になるまで待つ.
// 作成できたが待機に失敗したときは、作成したリスナーとエラーの両方を返す.
func (c *ConohaClient) CreateListener(ctx context.Context, opts CreateListenerOpts) (*LBListener, error) {
	var created *LBListener
	err := c.changeUnderLoadBalancer(ctx, opts.LoadBalancerID, func() error {
		var err error
		created, err = lbCreate[LBListener](ctx, c, "listener", opts, "listeners")
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := c.waitChildActive(ctx, opts.LoadBalancerID, "listener", created.ID, c.listenerStatus(created.ID)); err != nil {
		return created, err
	}
	l, err := c.GetListener(ctx, created.ID)
	if err != nil {
		return created, err
	}
	return l, nil
}

// GetListener はリスナーの詳細を取得する.
func (c *ConohaClient) GetListener(ctx context.Context, id string) (*LBListener, error) {
	return lbGet[LBListener](ctx, c, "listener", "listeners", id)
}

// UpdateListener はリスナー名を更新し、リスナーとロードバランサーが ACTIVE になるまで待つ.
func (c *ConohaClient) UpdateListener(ctx context.Context, id, loadBalancerID string, opts UpdateListenerOpts) (*LBListener, error) {
	err := c.changeUnderLoadBalancer(ctx, loadBalancerID, func() error {
		_, err := lbUpdate[LBListener](ctx, c, "listener", opts, "listeners", id)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := c.waitChildActive(ctx, loadBalancerID, "listener", id, c.listenerStatus(id)); err != nil {
		return nil, err
	}
	return c.GetListener(ctx, id)
}

// DeleteListener はリスナーを削除し、消えてロードバランサーが ACTIVE に戻るまで待つ.
func (c *ConohaClient) DeleteListener(ctx context.Context, id, loadBalancerID string) error {
	return c.deleteChild(ctx, loadBalancerID, "listener", id, c.listenerStatus(id), func() error {
		return lbDelete(ctx, c, "listeners", id)
	})
}

func (c *ConohaClient) listenerStatus(id string) statusFunc {
	return func(ctx context.Context) (string, error) {
		l, err := c.GetListener(ctx, id)
		if err != nil {
			return "", err
		}
		return l.ProvisioningStatus, nil
	}
}

// プール.

// CreatePool はプールを作成し、プールとロードバランサーが ACTIVE になるまで待つ.
// ロードバランサーの ID はリスナーから引く.
// 作成できたが待機に失敗したときは、作成したプールとエラーの両方を返す.
func (c *ConohaClient) CreatePool(ctx context.Context, opts CreatePoolOpts) (*LBPool, error) {
	listener, err := c.GetListener(ctx, opts.ListenerID)
	if err != nil {
		return nil, fmt.Errorf("failed to read listener %s: %w", opts.ListenerID, err)
	}
	lbID := listener.LoadBalancerID()

	var created *LBPool
	err = c.changeUnderLoadBalancer(ctx, lbID, func() error {
		var err error
		created, err = lbCreate[LBPool](ctx, c, "pool", opts, "pools")
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := c.waitChildActive(ctx, lbID, "pool", created.ID, c.poolStatus(created.ID)); err != nil {
		return created, err
	}
	p, err := c.GetPool(ctx, created.ID)
	if err != nil {
		return created, err
	}
	return p, nil
}

// GetPool はプールの詳細を取得する.
func (c *ConohaClient) GetPool(ctx context.Context, id string) (*LBPool, error) {
	return lbGet[LBPool](ctx, c, "pool", "pools", id)
}

// UpdatePool はプール名やバランシング方式を更新し、プールとロードバランサーが ACTIVE になるまで待つ.
// プールにメンバーが紐づいているときは、API がバランシング方式の変更を拒否する.
func (c *ConohaClient) UpdatePool(ctx context.Context, id, loadBalancerID string, opts UpdatePoolOpts) (*LBPool, error) {
	err := c.changeUnderLoadBalancer(ctx, loadBalancerID, func() error {
		_, err := lbUpdate[LBPool](ctx, c, "pool", opts, "pools", id)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := c.waitChildActive(ctx, loadBalancerID, "pool", id, c.poolStatus(id)); err != nil {
		return nil, err
	}
	return c.GetPool(ctx, id)
}

// DeletePool はプールを削除し、消えてロードバランサーが ACTIVE に戻るまで待つ.
func (c *ConohaClient) DeletePool(ctx context.Context, id, loadBalancerID string) error {
	return c.deleteChild(ctx, loadBalancerID, "pool", id, c.poolStatus(id), func() error {
		return lbDelete(ctx, c, "pools", id)
	})
}

func (c *ConohaClient) poolStatus(id string) statusFunc {
	return func(ctx context.Context) (string, error) {
		p, err := c.GetPool(ctx, id)
		if err != nil {
			return "", err
		}
		return p.ProvisioningStatus, nil
	}
}

// プールが紐づくロードバランサーの ID を引く.
func (c *ConohaClient) loadBalancerIDOfPool(ctx context.Context, poolID string) (string, error) {
	p, err := c.GetPool(ctx, poolID)
	if err != nil {
		return "", err
	}
	return p.LoadBalancerID(), nil
}

// メンバー.

// CreateMember はプールにメンバーを追加し、メンバーとロードバランサーが ACTIVE になるまで待つ.
// 追加できたが待機に失敗したときは、追加したメンバーとエラーの両方を返す.
func (c *ConohaClient) CreateMember(ctx context.Context, poolID string, opts CreateMemberOpts) (*LBMember, error) {
	lbID, err := c.loadBalancerIDOfPool(ctx, poolID)
	if err != nil {
		return nil, fmt.Errorf("failed to read pool %s: %w", poolID, err)
	}

	var created *LBMember
	err = c.changeUnderLoadBalancer(ctx, lbID, func() error {
		var err error
		created, err = lbCreate[LBMember](ctx, c, "member", opts, "pools", poolID, "members")
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := c.waitChildActive(ctx, lbID, "member", created.ID, c.memberStatus(poolID, created.ID)); err != nil {
		return created, err
	}
	m, err := c.GetMember(ctx, poolID, created.ID)
	if err != nil {
		return created, err
	}
	return m, nil
}

// GetMember はメンバーの詳細を取得する.
func (c *ConohaClient) GetMember(ctx context.Context, poolID, id string) (*LBMember, error) {
	return lbGet[LBMember](ctx, c, "member", "pools", poolID, "members", id)
}

// UpdateMember はメンバーのバランシングの有効状態を更新し、メンバーとロードバランサーが ACTIVE になるまで待つ.
func (c *ConohaClient) UpdateMember(ctx context.Context, poolID, id string, opts UpdateMemberOpts) (*LBMember, error) {
	lbID, err := c.loadBalancerIDOfPool(ctx, poolID)
	if err != nil {
		return nil, fmt.Errorf("failed to read pool %s: %w", poolID, err)
	}
	err = c.changeUnderLoadBalancer(ctx, lbID, func() error {
		_, err := lbUpdate[LBMember](ctx, c, "member", opts, "pools", poolID, "members", id)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := c.waitChildActive(ctx, lbID, "member", id, c.memberStatus(poolID, id)); err != nil {
		return nil, err
	}
	return c.GetMember(ctx, poolID, id)
}

// DeleteMember はメンバーを削除し、消えてロードバランサーが ACTIVE に戻るまで待つ.
// プールがすでに無いときは、メンバーも消えているものとして扱う.
func (c *ConohaClient) DeleteMember(ctx context.Context, poolID, id string) error {
	lbID, err := c.loadBalancerIDOfPool(ctx, poolID)
	if err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			return nil
		}
		return fmt.Errorf("failed to read pool %s: %w", poolID, err)
	}
	return c.deleteChild(ctx, lbID, "member", id, c.memberStatus(poolID, id), func() error {
		return lbDelete(ctx, c, "pools", poolID, "members", id)
	})
}

func (c *ConohaClient) memberStatus(poolID, id string) statusFunc {
	return func(ctx context.Context) (string, error) {
		m, err := c.GetMember(ctx, poolID, id)
		if err != nil {
			return "", err
		}
		return m.ProvisioningStatus, nil
	}
}

// ヘルスモニタ.

// CreateHealthMonitor はプールにヘルスモニタを作成し、ヘルスモニタとロードバランサーが ACTIVE になるまで待つ.
// 作成できたが待機に失敗したときは、作成したヘルスモニタとエラーの両方を返す.
func (c *ConohaClient) CreateHealthMonitor(ctx context.Context, opts CreateHealthMonitorOpts) (*LBHealthMonitor, error) {
	lbID, err := c.loadBalancerIDOfPool(ctx, opts.PoolID)
	if err != nil {
		return nil, fmt.Errorf("failed to read pool %s: %w", opts.PoolID, err)
	}

	var created *LBHealthMonitor
	err = c.changeUnderLoadBalancer(ctx, lbID, func() error {
		var err error
		created, err = lbCreate[LBHealthMonitor](ctx, c, "healthmonitor", opts, "healthmonitors")
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := c.waitChildActive(ctx, lbID, "health monitor", created.ID, c.healthMonitorStatus(created.ID)); err != nil {
		return created, err
	}
	m, err := c.GetHealthMonitor(ctx, created.ID)
	if err != nil {
		return created, err
	}
	return m, nil
}

// GetHealthMonitor はヘルスモニタの詳細を取得する.
func (c *ConohaClient) GetHealthMonitor(ctx context.Context, id string) (*LBHealthMonitor, error) {
	return lbGet[LBHealthMonitor](ctx, c, "healthmonitor", "healthmonitors", id)
}

// UpdateHealthMonitor はヘルスモニタ名を更新し、ヘルスモニタとロードバランサーが ACTIVE になるまで待つ.
func (c *ConohaClient) UpdateHealthMonitor(ctx context.Context, id, poolID string, opts UpdateHealthMonitorOpts) (*LBHealthMonitor, error) {
	lbID, err := c.loadBalancerIDOfPool(ctx, poolID)
	if err != nil {
		return nil, fmt.Errorf("failed to read pool %s: %w", poolID, err)
	}
	err = c.changeUnderLoadBalancer(ctx, lbID, func() error {
		_, err := lbUpdate[LBHealthMonitor](ctx, c, "healthmonitor", opts, "healthmonitors", id)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := c.waitChildActive(ctx, lbID, "health monitor", id, c.healthMonitorStatus(id)); err != nil {
		return nil, err
	}
	return c.GetHealthMonitor(ctx, id)
}

// DeleteHealthMonitor はヘルスモニタを削除し、消えてロードバランサーが ACTIVE に戻るまで待つ.
// プールがすでに無いときは、ヘルスモニタも消えているものとして扱う.
func (c *ConohaClient) DeleteHealthMonitor(ctx context.Context, id, poolID string) error {
	lbID, err := c.loadBalancerIDOfPool(ctx, poolID)
	if err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			return nil
		}
		return fmt.Errorf("failed to read pool %s: %w", poolID, err)
	}
	return c.deleteChild(ctx, lbID, "health monitor", id, c.healthMonitorStatus(id), func() error {
		return lbDelete(ctx, c, "healthmonitors", id)
	})
}

func (c *ConohaClient) healthMonitorStatus(id string) statusFunc {
	return func(ctx context.Context) (string, error) {
		m, err := c.GetHealthMonitor(ctx, id)
		if err != nil {
			return "", err
		}
		return m.ProvisioningStatus, nil
	}
}

// 共通の処理.

// statusFunc はリソースの provisioning_status を読む.
type statusFunc func(ctx context.Context) (string, error)

// ロードバランサーが PENDING_* でなくなるのを待ってから change を呼ぶ.
// ERROR のロードバランサーでも削除はできるため、事前の待機では ERROR を失敗にせず change に進む.
// change が 409 で拒否され、それがロードバランサーの変更中によるもの（応答が immutable を含む、
// またはロードバランサーが PENDING_* のまま）なら、待ってやり直す.
// それ以外の 409 は、重複などの本当の衝突なのでそのまま返す.
func (c *ConohaClient) changeUnderLoadBalancer(ctx context.Context, lbID string, change func() error) error {
	for {
		if err := c.waitLoadBalancerSettled(ctx, lbID); err != nil {
			return err
		}
		err := change()
		if err == nil || !gophercloud.ResponseCodeIs(err, 409) {
			return err
		}
		if !c.loadBalancerBusy(ctx, lbID, err) {
			return err
		}
		tflog.Debug(ctx, "Load balancer is busy; retrying the change after it settles.", map[string]any{
			"loadbalancer_id": lbID,
			"error":           err.Error(),
		})
		select {
		case <-ctx.Done():
			return err
		case <-time.After(time.Second):
		}
	}
}

// 409 がロードバランサーの変更中によるものかを判定する.
// 他の変更が反映し終わった直後だと、読み直したときにはもう ACTIVE のことがあるため、応答の文言も見る.
func (c *ConohaClient) loadBalancerBusy(ctx context.Context, lbID string, conflict error) bool {
	var unexpected gophercloud.ErrUnexpectedResponseCode
	if errors.As(conflict, &unexpected) && strings.Contains(strings.ToLower(string(unexpected.Body)), "immutable") {
		return true
	}
	lb, err := c.GetLoadBalancer(ctx, lbID)
	return err == nil && isPending(lb.ProvisioningStatus)
}

// ロードバランサーの provisioning_status が PENDING_* でなくなるまで待つ.
func (c *ConohaClient) waitLoadBalancerSettled(ctx context.Context, id string) error {
	return waitStatus(ctx, "load balancer", id, func(ctx context.Context) (string, error) {
		lb, err := c.GetLoadBalancer(ctx, id)
		if err != nil {
			return "", err
		}
		return lb.ProvisioningStatus, nil
	}, func(s string) bool { return !isPending(s) }) // waitStatus は done を ERROR 判定より先に見るため、ERROR でも待ち終わる
}

func isPending(status string) bool {
	return strings.HasPrefix(status, "PENDING_")
}

// 子リソースとロードバランサーの両方が ACTIVE になるまで待つ.
func (c *ConohaClient) waitChildActive(ctx context.Context, lbID, kind, id string, status statusFunc) error {
	if err := waitActive(ctx, kind, id, status); err != nil {
		return err
	}
	return c.WaitForLoadBalancerActive(ctx, lbID)
}

// 子リソースを削除し、消えてロードバランサーが ACTIVE に戻るまで待つ. すでに無ければ成功とする.
func (c *ConohaClient) deleteChild(ctx context.Context, lbID, kind, id string, status statusFunc, remove func() error) error {
	if err := c.changeUnderLoadBalancer(ctx, lbID, remove); err != nil {
		if gophercloud.ResponseCodeIs(err, 404) {
			return nil
		}
		return err
	}
	if err := waitGone(ctx, kind, id, status); err != nil {
		return err
	}
	err := c.WaitForLoadBalancerActive(ctx, lbID)
	if gophercloud.ResponseCodeIs(err, 404) {
		return nil
	}
	return err
}

// provisioning_status が ACTIVE になるまで待つ. ERROR になったら失敗とする.
func waitActive(ctx context.Context, kind, id string, status statusFunc) error {
	tflog.Debug(ctx, "Waiting for LBaaS resource to become ACTIVE.", map[string]any{"kind": kind, "id": id})
	return waitStatus(ctx, kind, id, status, func(s string) bool { return s == provisioningActive })
}

// GET が 404 を返す（または DELETED になる）まで待つ. ERROR になったら失敗とする.
func waitGone(ctx context.Context, kind, id string, status statusFunc) error {
	tflog.Debug(ctx, "Waiting for LBaaS resource to be deleted.", map[string]any{"kind": kind, "id": id})
	err := waitStatus(ctx, kind, id, status, func(s string) bool { return s == provisioningDeleted })
	if gophercloud.ResponseCodeIs(err, 404) {
		return nil
	}
	return err
}

func waitStatus(ctx context.Context, kind, id string, status statusFunc, done func(string) bool) error {
	last := ""
	err := gophercloud.WaitFor(ctx, func(ctx context.Context) (bool, error) {
		s, err := status(ctx)
		if err != nil {
			return false, err
		}
		last = s
		if done(s) {
			return true, nil
		}
		if s == provisioningError {
			return false, fmt.Errorf("%s %s is in ERROR provisioning status", kind, id)
		}
		return false, nil
	})
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("timed out waiting for %s %s (last provisioning_status: %q): %w", kind, id, last, err)
	}
	return err
}

func (c *ConohaClient) lbClient() (*gophercloud.ServiceClient, error) {
	if c.LoadBalancerClient == nil {
		return nil, ServiceUnavailable("load-balancer")
	}
	return c.LoadBalancerClient, nil
}

func lbURL(sc *gophercloud.ServiceClient, parts ...string) string {
	return sc.ServiceURL(append([]string{"lbaas"}, parts...)...)
}

// POST し、応答の key の中身を返す. 成功は 201.
func lbCreate[T any](ctx context.Context, c *ConohaClient, key string, body any, parts ...string) (*T, error) {
	sc, err := c.lbClient()
	if err != nil {
		return nil, err
	}
	var out map[string]T
	if _, err := sc.Post(ctx, lbURL(sc, parts...), map[string]any{key: body}, &out, &gophercloud.RequestOpts{OkCodes: []int{201}}); err != nil {
		tflog.Error(ctx, "LBaaS create request failed.", map[string]any{"path": strings.Join(parts, "/"), "error": err.Error()})
		return nil, err
	}
	return unwrap(out, key)
}

// GET し、応答の key の中身を返す. 成功は 200.
func lbGet[T any](ctx context.Context, c *ConohaClient, key string, parts ...string) (*T, error) {
	sc, err := c.lbClient()
	if err != nil {
		return nil, err
	}
	var out map[string]T
	if _, err := sc.Get(ctx, lbURL(sc, parts...), &out, &gophercloud.RequestOpts{OkCodes: []int{200}}); err != nil {
		return nil, err
	}
	return unwrap(out, key)
}

// PUT し、応答の key の中身を返す. 仕様の成功は 202 だが、OpenStack（Octavia）の 200 も成功とする.
func lbUpdate[T any](ctx context.Context, c *ConohaClient, key string, body any, parts ...string) (*T, error) {
	sc, err := c.lbClient()
	if err != nil {
		return nil, err
	}
	var out map[string]T
	if _, err := sc.Put(ctx, lbURL(sc, parts...), map[string]any{key: body}, &out, &gophercloud.RequestOpts{OkCodes: []int{200, 202}}); err != nil {
		tflog.Error(ctx, "LBaaS update request failed.", map[string]any{"path": strings.Join(parts, "/"), "error": err.Error()})
		return nil, err
	}
	return unwrap(out, key)
}

// DELETE する. 成功は 204.
func lbDelete(ctx context.Context, c *ConohaClient, parts ...string) error {
	sc, err := c.lbClient()
	if err != nil {
		return err
	}
	_, err = sc.Delete(ctx, lbURL(sc, parts...), &gophercloud.RequestOpts{OkCodes: []int{204}})
	return err
}

func unwrap[T any](out map[string]T, key string) (*T, error) {
	v, ok := out[key]
	if !ok {
		return nil, fmt.Errorf("the response has no %q object", key)
	}
	return &v, nil
}

func firstRef(refs []LBRef) string {
	if len(refs) == 0 {
		return ""
	}
	return refs[0].ID
}
