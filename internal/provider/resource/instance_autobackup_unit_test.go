// サーバーの自動バックアップのリソースの単体テストを提供する.
// 偽の ConoHa API で有効化・インポート・保存期間の変更（無効化してから有効化）・サーバー削除の検知・無効化を検証する.

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
	calls   []string           // 有効化・無効化の呼び出し順
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
		fakeapi.WriteJSON(w, http.StatusOK, map[string]any{"server": map[string]any{"id": sid, "name": sid, "status": "ACTIVE"}})
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

// 最後の有効化のリクエストの本文がドキュメントどおりであることを確かめる.
func (f *autoBackupFake) checkLastBody(retention float64) resource.TestCheckFunc {
	return func(*terraform.State) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		if len(f.bodies) == 0 {
			return fmt.Errorf("no enabling request was sent")
		}
		want := map[string]any{"backup": map[string]any{"instance_uuid": "srv-1", "schedule": "daily", "retention": retention}}
		if got := f.bodies[len(f.bodies)-1]; !reflect.DeepEqual(got, want) {
			g, _ := json.Marshal(got)
			return fmt.Errorf("enabling request body = %s, want instance_uuid, schedule daily and retention %v", g, retention)
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
			// 保存期間を変えると、無効化してから有効化し直す
			{
				Config: f.config("  retention = 30"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionDestroyBeforeCreate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("retention"), knownvalue.Int64Exact(30)),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					f.checkLastBody(30),
					f.checkEnabled(30),
					func(*terraform.State) error {
						f.mu.Lock()
						defer f.mu.Unlock()
						if want := []string{"enable", "disable", "enable"}; !reflect.DeepEqual(f.calls, want) {
							return fmt.Errorf("calls = %v, want %v", f.calls, want)
						}
						return nil
					},
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
			// サーバーが消えたら State から外し、作り直す計画になる
			{
				PreConfig: func() {
					f.mu.Lock()
					defer f.mu.Unlock()
					delete(f.servers, "srv-1")
				},
				Config:             f.config("  retention = 30"),
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
				Config: f.config("  retention = 30"),
				Check:  f.checkEnabled(30),
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
