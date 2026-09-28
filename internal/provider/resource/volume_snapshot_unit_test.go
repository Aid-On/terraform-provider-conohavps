// ボリュームのスナップショットのリソースの単体テストを提供する.
// 偽の ConoHa API でスナップショットの作成・インポート・再作成・自動削除後の再作成・削除を検証する.

package resource_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
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

// スナップショットの状態を持つ偽の API.
type snapshotFake struct {
	*fakeapi.Server
	mu        sync.Mutex
	snapshots map[string]map[string]any
	polls     map[string]int   // スナップショット ID → 取得された回数
	bodies    []map[string]any // 作成のリクエストの本文
}

func newSnapshotFake(t *testing.T) *snapshotFake {
	f := &snapshotFake{Server: fakeapi.New(t), snapshots: map[string]map[string]any{}, polls: map[string]int{}}

	f.Mux.HandleFunc("POST /block-storage/v3/tenant/snapshots", func(w http.ResponseWriter, r *http.Request) {
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
		// スナップショットは1つのみ作成できる
		if len(f.snapshots) > 0 {
			fakeapi.WriteJSON(w, http.StatusBadRequest, map[string]any{"badRequest": map[string]any{"message": "snapshot limit exceeded"}})
			return
		}
		req := body["snapshot"].(map[string]any)
		id := f.NewID("snap")
		s := map[string]any{
			"id": id, "status": "creating", "name": req["name"], "description": req["description"],
			"volume_id": req["volume_id"], "size": 100, "user_id": "user",
			"os-extended-snapshot-attributes:project_id": fakeapi.TenantID,
			"created_at": "2018-11-28T06:25:15.000000", "updated_at": nil,
		}
		f.snapshots[id] = s
		fakeapi.WriteJSON(w, http.StatusAccepted, map[string]any{"snapshot": s})
	})

	f.Mux.HandleFunc("GET /block-storage/v3/tenant/snapshots/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !fakeapi.Authorized(w, r) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		id := r.PathValue("id")
		s, ok := f.snapshots[id]
		if !ok {
			fakeapi.WriteJSON(w, http.StatusNotFound, map[string]any{"itemNotFound": map[string]any{"message": "snapshot not found"}})
			return
		}
		// 1回目の取得までは作成中を返し、プロバイダが available を待つことを確かめる
		f.polls[id]++
		if f.polls[id] > 1 {
			s["status"] = "available"
			s["os-extended-snapshot-attributes:progress"] = "100%"
			s["updated_at"] = "2018-11-28T06:26:10.000000"
		}
		fakeapi.WriteJSON(w, http.StatusOK, map[string]any{"snapshot": s})
	})

	f.Mux.HandleFunc("DELETE /block-storage/v3/tenant/snapshots/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !fakeapi.Authorized(w, r) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		id := r.PathValue("id")
		s, ok := f.snapshots[id]
		if !ok {
			fakeapi.WriteJSON(w, http.StatusNotFound, map[string]any{"itemNotFound": map[string]any{"message": "snapshot not found"}})
			return
		}
		// 削除できるのは available か error のときだけ
		if st := s["status"]; st != "available" && st != "error" {
			fakeapi.WriteJSON(w, http.StatusBadRequest, map[string]any{"badRequest": map[string]any{"message": "invalid status"}})
			return
		}
		delete(f.snapshots, id)
		w.WriteHeader(http.StatusAccepted)
	})
	return f
}

// 最後の作成のリクエストの本文が want と一致することを確かめる.
func (f *snapshotFake) checkLastBody(want map[string]any) resource.TestCheckFunc {
	return func(*terraform.State) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		if len(f.bodies) == 0 {
			return fmt.Errorf("no snapshot creation request was sent")
		}
		if got := f.bodies[len(f.bodies)-1]; !reflect.DeepEqual(got, map[string]any{"snapshot": want}) {
			g, _ := json.Marshal(got)
			w, _ := json.Marshal(map[string]any{"snapshot": want})
			return fmt.Errorf("snapshot request body = %s, want %s", g, w)
		}
		return nil
	}
}

func TestVolumeSnapshotResource_Unit(t *testing.T) {
	f := newSnapshotFake(t)
	const addr = "conohavps_volume_snapshot.test"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: fakeapi.Factories,
		CheckDestroy: func(*terraform.State) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			if n := len(f.snapshots); n != 0 {
				return fmt.Errorf("%d snapshots still exist", n)
			}
			return nil
		},
		Steps: []resource.TestStep{
			// 説明つきで作成し、available になるまで待つ
			{
				Config: f.ProviderConfig() + `
resource "conohavps_volume_snapshot" "test" {
  volume_id   = "vol-1"
  name        = "the-snapshot-name"
  description = "test snapshot"
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionCreate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("status"), knownvalue.StringExact("available")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("size"), knownvalue.Int64Exact(100)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("created_at"), knownvalue.StringExact("2018-11-28T06:25:15Z")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("updated_at"), knownvalue.StringExact("2018-11-28T06:26:10Z")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("volume_id"), knownvalue.StringExact("vol-1")),
				},
				Check: f.checkLastBody(map[string]any{"volume_id": "vol-1", "name": "the-snapshot-name", "description": "test snapshot"}),
			},
			// インポート
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateVerify: true,
			},
			// 更新 API は無いので、名前を変えると作り直す. 説明を外すと本文に description を含めない
			{
				Config: f.ProviderConfig() + `
resource "conohavps_volume_snapshot" "test" {
  volume_id = "vol-1"
  name      = "renamed"
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionDestroyBeforeCreate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("renamed")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.Null()),
				},
				Check: f.checkLastBody(map[string]any{"volume_id": "vol-1", "name": "renamed"}),
			},
			// 24時間経過で自動削除されたら、State から外して作り直す
			{
				PreConfig: func() {
					f.mu.Lock()
					defer f.mu.Unlock()
					clear(f.snapshots)
				},
				Config: f.ProviderConfig() + `
resource "conohavps_volume_snapshot" "test" {
  volume_id = "vol-1"
  name      = "renamed"
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionCreate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("status"), knownvalue.StringExact("available")),
				},
			},
		},
	})
}
