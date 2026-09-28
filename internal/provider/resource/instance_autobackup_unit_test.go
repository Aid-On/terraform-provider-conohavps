// サーバーの自動バックアップのリソースの単体テストを提供する.
// 偽の ConoHa API で有効化・インポート・保存期間のその場での変更・サーバー削除の検知・解約を検証する.

package resource_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"sync"
	"testing"

	"github.com/gmo-internet/terraform-provider-conohavps/internal/provider/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// 自動バックアップの状態を持つ偽の API.
type autoBackupFake struct {
	*fakeapi.Server
	mu      sync.Mutex
	servers map[string]bool
	enabled map[string]float64 // サーバー ID → 保存期間
	bodies  []map[string]any   // 有効化のリクエストの本文
	updates []map[string]any   // 保存期間の変更のリクエストの本文
	calls   []string           // 有効化・保存期間の変更・解約の呼び出し順
}

func newAutoBackupFake(t *testing.T) *autoBackupFake {
	f := &autoBackupFake{Server: fakeapi.New(t), servers: map[string]bool{"srv-1": true}, enabled: map[string]float64{}}

	f.Mux.HandleFunc("POST /block-storage/v3/tenant/backups", func(w http.ResponseWriter, r *http.Request) {
		if !fakeapi.Authorized(w, r) {
			return
		}
		var body map[string]any
		if err := fakeapi.ReadJSON(r, &body); err != nil {
			fakeapi.WriteJSON(w, http.StatusBadRequest, map[string]any{"badRequest": map[string]any{"message": err.Error()}})
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.bodies = append(f.bodies, body)
		req := body["backup"].(map[string]any)
		sid, _ := req["instance_uuid"].(string)
		if !f.servers[sid] {
			fakeapi.WriteJSON(w, http.StatusNotFound, map[string]any{"itemNotFound": map[string]any{"message": "server not found"}})
			return
		}
		if _, ok := f.enabled[sid]; ok {
			fakeapi.WriteJSON(w, http.StatusConflict, map[string]any{"conflictingRequest": map[string]any{"message": "already enabled"}})
			return
		}
		f.enabled[sid], _ = req["retention"].(float64)
		f.calls = append(f.calls, "enable")
		fakeapi.WriteJSON(w, http.StatusCreated, map[string]any{"backup": map[string]any{"instance_uuid": sid, "id": f.NewID("backup")}})
	})

	// 保存期間の変更（block-storage/update-backup）. 日次バックアップを申し込んでいないサーバーには 404 を返す
	f.Mux.HandleFunc("PUT /block-storage/v3/tenant/backups/{sid}", func(w http.ResponseWriter, r *http.Request) {
		if !fakeapi.Authorized(w, r) {
			return
		}
		var body map[string]any
		if err := fakeapi.ReadJSON(r, &body); err != nil {
			fakeapi.WriteJSON(w, http.StatusBadRequest, map[string]any{"badRequest": map[string]any{"message": err.Error()}})
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.updates = append(f.updates, body)
		sid := r.PathValue("sid")
		if _, ok := f.enabled[sid]; !ok {
			fakeapi.WriteJSON(w, http.StatusNotFound, map[string]any{"itemNotFound": map[string]any{"message": "backup not found"}})
			return
		}
		retention, _ := body["backup"].(map[string]any)["retention"].(float64)
		if retention < 14 || retention > 30 {
			fakeapi.WriteJSON(w, http.StatusBadRequest, map[string]any{"badRequest": map[string]any{"message": "invalid retention"}})
			return
		}
		f.enabled[sid] = retention
		f.calls = append(f.calls, "update")
		fakeapi.WriteJSON(w, http.StatusOK, map[string]any{"backup": map[string]any{"retention": retention}})
	})

	f.Mux.HandleFunc("DELETE /block-storage/v3/tenant/backups/{sid}", func(w http.ResponseWriter, r *http.Request) {
		if !fakeapi.Authorized(w, r) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		sid := r.PathValue("sid")
		if !f.servers[sid] {
			fakeapi.WriteJSON(w, http.StatusNotFound, map[string]any{"itemNotFound": map[string]any{"message": "server not found"}})
			return
		}
		delete(f.enabled, sid)
		f.calls = append(f.calls, "disable")
		w.WriteHeader(http.StatusNoContent)
	})

	f.Mux.HandleFunc("GET /compute/v2.1/servers/{sid}", func(w http.ResponseWriter, r *http.Request) {
		if !fakeapi.Authorized(w, r) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		sid := r.PathValue("sid")
		if !f.servers[sid] {
			fakeapi.WriteJSON(w, http.StatusNotFound, map[string]any{"itemNotFound": map[string]any{"message": "server not found"}})
			return
		}
		// 自動バックアップが有効なサーバーは、メタデータに backup_status などが載る（サーバー詳細取得のドキュメントの応答例. API 仕様には定義が無い）
		metadata := map[string]any{"instance_name_tag": sid}
		if _, ok := f.enabled[sid]; ok {
			metadata["backup_status"] = "active"
			metadata["backup_id"] = "vol-boot"
			metadata["backup_set"] = "6"
			metadata["backup_rotate"] = "3"
		}
		fakeapi.WriteJSON(w, http.StatusOK, map[string]any{"server": map[string]any{"id": sid, "name": sid, "status": "ACTIVE", "metadata": metadata}})
	})
	return f
}

func (f *autoBackupFake) config(retention string) string {
	return f.ProviderConfig() + `
resource "conohavps_instance_autobackup" "test" {
  instance_id = "srv-1"
` + retention + `
}
`
}

// 最後の有効化のリクエストの本文が API 仕様どおりであることを確かめる（非推奨の schedule は送らない）.
func (f *autoBackupFake) checkLastBody(retention float64) resource.TestCheckFunc {
	return func(*terraform.State) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		if len(f.bodies) == 0 {
			return fmt.Errorf("no enabling request was sent")
		}
		want := map[string]any{"backup": map[string]any{"instance_uuid": "srv-1", "retention": retention}}
		if got := f.bodies[len(f.bodies)-1]; !reflect.DeepEqual(got, want) {
			g, _ := json.Marshal(got)
			return fmt.Errorf("enabling request body = %s, want instance_uuid and retention %v", g, retention)
		}
		return nil
	}
}

// 最後の保存期間の変更のリクエストの本文が API 仕様どおりであることを確かめる.
func (f *autoBackupFake) checkLastUpdate(retention float64) resource.TestCheckFunc {
	return func(*terraform.State) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		if len(f.updates) == 0 {
			return fmt.Errorf("no retention update request was sent")
		}
		want := map[string]any{"backup": map[string]any{"retention": retention}}
		if got := f.updates[len(f.updates)-1]; !reflect.DeepEqual(got, want) {
			g, _ := json.Marshal(got)
			return fmt.Errorf("retention update request body = %s, want retention %v", g, retention)
		}
		return nil
	}
}

// 有効化・保存期間の変更・解約の呼び出し順を確かめる.
func (f *autoBackupFake) checkCalls(want ...string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		if !reflect.DeepEqual(f.calls, want) {
			return fmt.Errorf("calls = %v, want %v", f.calls, want)
		}
		return nil
	}
}

func (f *autoBackupFake) checkEnabled(retention float64) resource.TestCheckFunc {
	return func(*terraform.State) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		if got, ok := f.enabled["srv-1"]; !ok || got != retention {
			return fmt.Errorf("auto-backup of srv-1 = (%v, %v), want enabled with retention %v", got, ok, retention)
		}
		return nil
	}
}

func TestInstanceAutoBackupResource_Unit(t *testing.T) {
	f := newAutoBackupFake(t)
	const addr = "conohavps_instance_autobackup.test"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		CheckDestroy: func(*terraform.State) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			if _, ok := f.enabled["srv-1"]; ok {
				return fmt.Errorf("auto-backup of srv-1 is still enabled")
			}
			return nil
		},
		Steps: []resource.TestStep{
			// 既定値（daily・14日）で有効化する
			{
				Config: f.config(""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionCreate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringExact("srv-1")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("schedule"), knownvalue.StringExact("daily")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("retention"), knownvalue.Int64Exact(14)),
				},
				Check: resource.ComposeAggregateTestCheckFunc(f.checkLastBody(14), f.checkEnabled(14)),
			},
			// サーバー ID だけで取り込むと保存期間は既定値の 14 になる
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateVerify: true,
			},
			// 非推奨の schedule を daily と書いた既存の設定は、差分にならない
			{
				Config: f.config(`  schedule = "daily"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			// 保存期間を変えると、解約・再申し込みをせずに更新 API でその場で変える
			{
				Config: f.config("  retention = 30"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringExact("srv-1")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("retention"), knownvalue.Int64Exact(30)),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					f.checkLastUpdate(30),
					f.checkEnabled(30),
					f.checkCalls("enable", "update"),
				),
			},
			// `<instance_id>/<retention>` で保存期間ごと取り込む
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateId:     "srv-1/30",
				ImportStateVerify: true,
			},
			// 保存期間は 14〜30 日
			{
				Config:      f.config("  retention = 13"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)retention.*between 14 and 30`),
			},
			{
				Config:      f.config(`  schedule = "weekly"`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`must be one of \[daily\]`),
			},
			// コントロールパネルなどで無効にされたら、backup_status が消えるので作り直す計画になる
			{
				PreConfig: func() {
					f.mu.Lock()
					defer f.mu.Unlock()
					delete(f.enabled, "srv-1")
				},
				Config:             f.config("  retention = 30"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			// 適用すると有効に戻る
			{
				Config: f.config("  retention = 30"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(addr, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeTestCheckFunc(f.checkEnabled(30), f.checkLastBody(30)),
			},
			// 保存期間を書かなくなると、既定値の 14 にその場で戻す
			{
				Config: f.config(""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("retention"), knownvalue.Int64Exact(14)),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					f.checkLastUpdate(14),
					f.checkEnabled(14),
					f.checkCalls("enable", "update", "enable", "update"),
				),
			},
			// サーバーが消えたら State から外し、作り直す計画になる
			{
				PreConfig: func() {
					f.mu.Lock()
					defer f.mu.Unlock()
					delete(f.servers, "srv-1")
				},
				Config:             f.config(""),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			// サーバーを戻すと差分は無く、最後の削除で無効化されることを CheckDestroy で確かめる
			{
				PreConfig: func() {
					f.mu.Lock()
					defer f.mu.Unlock()
					f.servers["srv-1"] = true
				},
				Config: f.config(""),
				Check:  f.checkEnabled(14),
			},
		},
	})
}

func TestInstanceAutoBackupResource_InvalidImportID(t *testing.T) {
	f := newAutoBackupFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		Steps: []resource.TestStep{{
			Config:        f.config(""),
			ResourceName:  "conohavps_instance_autobackup.test",
			ImportState:   true,
			ImportStateId: "srv-1/7",
			ExpectError:   regexp.MustCompile(`retention from 14 to 30`),
		}},
	})
}
