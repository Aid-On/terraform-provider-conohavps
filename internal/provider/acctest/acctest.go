// 受け入れテストに使用するプロバイダ、クライアント、およびテスト共通のメソッドを提供する.

package acctest

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/keypairs"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/db/v1/instances"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/rules"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider"
	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/service"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

var TestAccClient = &service.ConohaClient{}
var TestAccProtoV6ProviderFactories map[string]func() (tfprotov6.ProviderServer, error)

var TestAccResourcePrefix = "tf-acctest"

// 受け入れテストに使用するプロバイダとクライアントを初期化する.
func init() {
	TestAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
		"conohavps": providerserver.NewProtocol6WithError(provider.New("0.0.0-test")()),
	}

	// リージョンと認証エンドポイントのみ標準値を設定する
	var region = "c3j1"
	var identityEndpoint = "https://identity.c3j1.conoha.io/v3"

	// 環境変数から初期化に必要なパラメータを受け入れる
	// リージョンと認証エンドポイントは、環境変数が空文字でない場合のみ、標準値を環境変数で上書きする
	if os.Getenv("CONOHAVPS_REGION") != "" {
		region = os.Getenv("CONOHAVPS_REGION")
	}

	if os.Getenv("CONOHAVPS_IDENTITY_ENDPOINT") != "" {
		identityEndpoint = os.Getenv("CONOHAVPS_IDENTITY_ENDPOINT")
	}

	authOpts := gophercloud.AuthOptions{
		UserID:           os.Getenv("CONOHAVPS_USER_ID"),
		Password:         os.Getenv("CONOHAVPS_PASSWORD"),
		TenantID:         os.Getenv("CONOHAVPS_TENANT_ID"),
		IdentityEndpoint: identityEndpoint,
	}

	// 受け入れテスト（TF_ACC=1）のときだけ実際の API に認証する.
	// それ以外では認証しないので、偽の API を使うテストは認証情報なしで同じパッケージで動く.
	if os.Getenv("TF_ACC") == "" {
		return
	}

	err := TestAccClient.Authenticate(context.Background(), authOpts, region)
	if err != nil {
		panic(err)
	}
}

// テスト実行前に満たすべき制約を確認する.
func TestAccPreCheck(t *testing.T) {
	if TestAccClient.BlockStorageClient == nil || TestAccClient.ComputeClient == nil || TestAccClient.NetworkClient == nil {
		t.Fatal("an error occurred during the initialization of the required component clients.")
	}
}

// 指定した秒数だけ待機する.
func TestAccSleep(seconds int) {
	time.Sleep(time.Duration(seconds) * time.Second)
}

// 受け入れテストで作成されたリソースが削除されているかを確認する.
func TestAccCheckResourceDestroy(t *testing.T) func(s *terraform.State) error {
	return func(s *terraform.State) error {
		for _, rs := range s.RootModule().Resources {
			switch rs.Type {
			case "conohavps_instance":
				_, err := instances.Get(t.Context(), TestAccClient.ComputeClient, rs.Primary.ID).Extract()
				if err == nil {
					return fmt.Errorf("an acceptance test resource (instance) still exists in your ConoHa VPS account, ID: (%s)", rs.Primary.ID)
				}
			case "conohavps_keypair":
				_, err := keypairs.Get(t.Context(), TestAccClient.ComputeClient, rs.Primary.ID, nil).Extract()
				if err == nil {
					return fmt.Errorf("an acceptance test resource (keypair) still exists in your ConoHa VPS account, Name: (%s)", rs.Primary.ID)
				}
			case "conohavps_volume":
				_, err := volumes.Get(t.Context(), TestAccClient.BlockStorageClient, rs.Primary.ID).Extract()
				if err == nil {
					return fmt.Errorf("an acceptance test resource (volume) still exists in your ConoHa VPS account, ID: (%s)", rs.Primary.ID)
				}
			case "conohavps_securitygroup":
				_, err := rules.Get(t.Context(), TestAccClient.NetworkClient, rs.Primary.ID).Extract()
				if err == nil {
					return fmt.Errorf("an acceptance test resource (security group) still exists in your ConoHa VPS account, ID: (%s)", rs.Primary.ID)
				}
			case "conohavps_securitygroup_rule":
				_, err := groups.Get(t.Context(), TestAccClient.NetworkClient, rs.Primary.ID).Extract()
				if err == nil {
					return fmt.Errorf("an acceptance test resource (security group rule) still exists in your ConoHa VPS account, ID: (%s)", rs.Primary.ID)
				}
			default:
				continue
			}
		}
		return nil
	}
}

// 受け入れテスト固有の接頭辞を返す.
func GetTestPrefix(resourceType string) string {
	return fmt.Sprintf("%s-%s", TestAccResourcePrefix, resourceType)
}

// リソース毎に一覧を取得し、受け入れテスト固有の接頭辞をもつリソースを削除する.
func TestSweepResources(prefix string) func(string) error {
	return func(shortRegion string) error {
		ctx := context.Background()
		timeoutCtx, cancel := context.WithTimeout(ctx, 300*time.Second)
		defer cancel()

		// インスタンス
		instanceList, err := servers.List(TestAccClient.ComputeClient, servers.ListOpts{}).AllPages(ctx)
		if err != nil {
			return fmt.Errorf("failed to retrieve instance list, error: %w", err)
		}
		allInstances, err := servers.ExtractServers(instanceList)
		if err != nil {
			return fmt.Errorf("failed to extract instances, error: %w", err)
		}
		for _, i := range allInstances {
			if strings.HasPrefix(i.Metadata["instance_name_tag"], prefix) {
				err := servers.Delete(ctx, TestAccClient.ComputeClient, i.ID).ExtractErr()
				if err != nil {
					if gophercloud.ResponseCodeIs(err, 404) {
						continue
					}
					return fmt.Errorf("failed to delete instance (%s), error: %w", i.ID, err)
				}
			}
		}

		// ボリューム
		volumeList, err := volumes.List(TestAccClient.BlockStorageClient, volumes.ListOpts{}).AllPages(ctx)
		if err != nil {
			return fmt.Errorf("failed to retrieve volume list, error: %w", err)
		}

		allVolumes, err := volumes.ExtractVolumes(volumeList)
		if err != nil {
			return fmt.Errorf("failed to extract volumes, error: %w", err)
		}

		for _, v := range allVolumes {
			if strings.HasPrefix(v.Name, prefix) {
				// ボリュームのステータスが available ならボリュームを削除できる
				// ボリュームのステータスが available でなければ待機する
				err := volumes.WaitForStatus(timeoutCtx, TestAccClient.BlockStorageClient, v.ID, "available")
				if err != nil {
					return fmt.Errorf("an unexpected error occurred while waiting for volume (%s) to become available, error: %w", v.ID, err)
				}

				err = volumes.Delete(ctx, TestAccClient.BlockStorageClient, v.ID, volumes.DeleteOpts{}).ExtractErr()
				if err != nil {
					if gophercloud.ResponseCodeIs(err, 404) {
						continue
					}
					return fmt.Errorf("failed to delete volume (%s), error: %w", v.ID, err)
				}
			}
		}

		// キーペア
		keypairList, err := keypairs.List(TestAccClient.ComputeClient, keypairs.ListOpts{}).AllPages(ctx)
		if err != nil {
			return fmt.Errorf("failed to retrieve keypair list, error: %w", err)
		}

		allKeypairs, err := keypairs.ExtractKeyPairs(keypairList)
		if err != nil {
			return fmt.Errorf("failed to extract keypairs, error: %w", err)
		}

		for _, k := range allKeypairs {
			if strings.HasPrefix(k.Name, prefix) {
				err := keypairs.Delete(ctx, TestAccClient.ComputeClient, k.Name, nil).ExtractErr()
				if err != nil {
					if gophercloud.ResponseCodeIs(err, 404) {
						continue
					}
					return fmt.Errorf("failed to delete keypair (%s), error: %w", k.Name, err)
				}
			}
		}

		// セキュリティグループ、セキュリティグループルール
		securityGroupList, err := groups.List(TestAccClient.NetworkClient, groups.ListOpts{}).AllPages(ctx)
		if err != nil {
			return fmt.Errorf("failed to retrieve security group list, error: %w", err)
		}

		allSecurityGroups, err := groups.ExtractGroups(securityGroupList)
		if err != nil {
			return fmt.Errorf("failed to extract security groups, error: %w", err)
		}

		var targetGroupIDs []string

		for _, sg := range allSecurityGroups {
			if strings.HasPrefix(sg.Name, prefix) {
				targetGroupIDs = append(targetGroupIDs, sg.ID)
			}
		}

		// セキュリティグループを削除する前に、セキュリティグループに紐づくセキュリティグループルールを削除する
		for _, groupID := range targetGroupIDs {
			ruleList, err := rules.List(TestAccClient.NetworkClient, rules.ListOpts{
				SecGroupID: groupID,
			}).AllPages(ctx)
			if err != nil {
				return fmt.Errorf("failed to retrieve security group rule list associated with the security group ID (%s), error: %w", groupID, err)
			}

			allRules, err := rules.ExtractRules(ruleList)
			if err != nil {
				return fmt.Errorf("failed to extract security group rules associated with the security group ID (%s), error: %w", groupID, err)
			}

			for _, rule := range allRules {
				err := rules.Delete(ctx, TestAccClient.NetworkClient, rule.ID).ExtractErr()
				if err != nil {
					if gophercloud.ResponseCodeIs(err, 404) {
						continue
					}
					return fmt.Errorf("failed to delete security group rule (%s), error: %w", rule.ID, err)
				}
			}
		}
		for _, sg := range targetGroupIDs {
			err := groups.Delete(ctx, TestAccClient.NetworkClient, sg).ExtractErr()
			if err != nil {
				if gophercloud.ResponseCodeIs(err, 404) {
					continue
				}
				return fmt.Errorf("failed to delete security group (%s), error: %w", sg, err)
			}
		}

		return nil
	}
}
