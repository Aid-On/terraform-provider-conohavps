// バックアップ一覧のデータソースのテストを提供する.
// 偽の ConoHa API にバックアップ詳細一覧を足し、サーバー・ボリュームでの絞り込みと並び順を検証する.

package datasource_test

import (
	"net/http"
	"testing"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func withBackups(t *testing.T) *fakeapi.Server {
	s := fakeapi.New(t)
	backup := func(id, sid, vid, boot, created string) map[string]any {
		return map[string]any{
			"id": id, "status": "available", "size": 100, "object_count": 0, "availability_zone": nil,
			"container": "cinder.backups", "created_at": created, "updated_at": created,
			"name": "auto-backup-" + sid + "_vda_" + vid, "description": nil, "fail_reason": nil, "volume_id": vid,
			"is_incremental": false, "has_dependent_backups": false, "snapshot_id": nil, "data_timestamp": created,
			"metadata": map[string]any{"instance_uuid": sid, "is_boot_volume": boot, "os_type": "linux", "os_version": "NotFound"},
		}
	}
	s.Mux.HandleFunc("GET /block-storage/v3/tenant/backups/detail", func(w http.ResponseWriter, r *http.Request) {
		if !fakeapi.Authorized(w, r) {
			return
		}
		fakeapi.WriteJSON(w, http.StatusOK, map[string]any{"backups": []map[string]any{
			backup("b-old", "srv-1", "vol-boot", "True", "2023-11-24T00:02:50.000000"),
			backup("b-new", "srv-1", "vol-boot", "True", "2023-11-25T00:02:50.000000"),
			backup("b-add", "srv-1", "vol-add", "False", "2023-11-25T00:03:50.000000"),
			backup("b-other", "srv-2", "vol-other", "True", "2023-11-25T00:02:50.000000"),
		}})
	})
	return s
}

func TestBackupsDataSource(t *testing.T) {
	s := withBackups(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps: []resource.TestStep{{
			Config: s.ProviderConfig() + `
data "conohavps_backups" "server" {
  instance_id = "srv-1"
}

data "conohavps_backups" "boot" {
  instance_id = "srv-1"
  volume_id   = "vol-boot"
}

data "conohavps_backups" "all" {}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				// サーバーで絞り込み、新しい順に並ぶ
				statecheck.ExpectKnownValue("data.conohavps_backups.server", tfjsonpath.New("backups"), knownvalue.ListExact([]knownvalue.Check{
					knownvalue.ObjectPartial(map[string]knownvalue.Check{
						"id": knownvalue.StringExact("b-add"), "is_boot_volume": knownvalue.Bool(false), "volume_id": knownvalue.StringExact("vol-add"),
					}),
					knownvalue.ObjectPartial(map[string]knownvalue.Check{
						"id": knownvalue.StringExact("b-new"), "is_boot_volume": knownvalue.Bool(true), "instance_id": knownvalue.StringExact("srv-1"),
						"size": knownvalue.Int64Exact(100), "status": knownvalue.StringExact("available"),
					}),
					knownvalue.ObjectPartial(map[string]knownvalue.Check{"id": knownvalue.StringExact("b-old")}),
				})),
				statecheck.ExpectKnownValue("data.conohavps_backups.boot", tfjsonpath.New("backups"), knownvalue.ListSizeExact(2)),
				statecheck.ExpectKnownValue("data.conohavps_backups.all", tfjsonpath.New("backups"), knownvalue.ListSizeExact(4)),
			},
		}},
	})
}
